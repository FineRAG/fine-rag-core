package retrieval

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/logging"

	"go.uber.org/zap"
)

// Service defines retrieval boundaries for query execution and reranking.
type Service interface {
	Search(ctx context.Context, metadata contracts.RequestMetadata, query contracts.RetrievalQuery) (contracts.RetrievalResult, error)
	Rerank(ctx context.Context, metadata contracts.RequestMetadata, req contracts.RerankRequest) ([]contracts.RerankCandidate, error)
}

type Config struct {
	RerankTopK        int
	RerankTimeout     time.Duration
	FailureThreshold  int
	Clock             func() time.Time
	EmbeddingProvider contracts.EmbeddingProvider
	QueryRewriter     contracts.QueryRewriter
}

type vectorEmbeddingSearcher interface {
	SearchByEmbedding(ctx context.Context, tenantID contracts.TenantID, queryEmbedding []float32, topK int) ([]contracts.RetrievalDocument, error)
}

type DeterministicRetrievalService struct {
	searcher      contracts.VectorSearcher
	reranker      contracts.Reranker
	embedder      contracts.EmbeddingProvider
	queryRewriter contracts.QueryRewriter
	clock         func() time.Time

	rerankTopK       int
	rerankTimeout    time.Duration
	failureThreshold int

	mu                        sync.Mutex
	consecutiveRerankFailures int
}

func NewDeterministicRetrievalService(searcher contracts.VectorSearcher, reranker contracts.Reranker, cfg Config) *DeterministicRetrievalService {
	rerankTopK := cfg.RerankTopK
	if rerankTopK <= 0 {
		rerankTopK = 5
	}
	rerankTimeout := cfg.RerankTimeout
	if rerankTimeout <= 0 {
		rerankTimeout = 200 * time.Millisecond
	}
	failureThreshold := cfg.FailureThreshold
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	clock := cfg.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC().Round(0) }
	}

	return &DeterministicRetrievalService{
		searcher:         searcher,
		reranker:         reranker,
		embedder:         cfg.EmbeddingProvider,
		queryRewriter:    cfg.QueryRewriter,
		clock:            clock,
		rerankTopK:       rerankTopK,
		rerankTimeout:    rerankTimeout,
		failureThreshold: failureThreshold,
	}
}

func (s *DeterministicRetrievalService) Search(ctx context.Context, metadata contracts.RequestMetadata, query contracts.RetrievalQuery) (contracts.RetrievalResult, error) {
	start := s.clock()
	if err := metadata.Validate(); err != nil {
		return contracts.RetrievalResult{}, contracts.WrapValidationErr("request_metadata", err)
	}
	if err := query.Validate(); err != nil {
		return contracts.RetrievalResult{}, contracts.WrapValidationErr("retrieval_query", err)
	}
	if metadata.TenantID != query.TenantID {
		return contracts.RetrievalResult{}, errors.New("tenant mismatch between metadata and retrieval query")
	}
	if s.searcher == nil {
		return contracts.RetrievalResult{}, errors.New("vector searcher is required")
	}

	docs, effectiveQuery, err := s.searchDocuments(ctx, query)
	if err != nil {
		return contracts.RetrievalResult{}, err
	}

	filtered := make([]contracts.RetrievalDocument, 0, len(docs))
	for _, doc := range docs {
		if doc.TenantID == query.TenantID {
			filtered = append(filtered, doc)
		}
	}
	filtered = applyMetadataIntentRanking(effectiveQuery, filtered)

	rerankApplied := false
	fallbackReason := ""
	if len(filtered) > 0 {
		ranked, rankErr := s.rerankDocuments(ctx, query, filtered)
		if rankErr == nil {
			filtered = ranked
			rerankApplied = s.reranker != nil
		} else {
			fallbackReason = rankErr.Error()
		}
	}

	result := contracts.RetrievalResult{
		TenantID:  query.TenantID,
		RequestID: query.RequestID,
		Documents: filtered,
		Citations: buildCitations(filtered),
		Trace: contracts.RetrievalTrace{
			TenantFilterApplied: true,
			CandidateCount:      len(filtered),
			RerankApplied:       rerankApplied,
			FallbackReason:      fallbackReason,
			DurationMillis:      s.clock().Sub(start).Milliseconds(),
		},
	}
	if tracer, ok := s.searcher.(contracts.VectorTraceProvider); ok {
		vt := tracer.LastVectorTrace()
		result.Trace.VectorProvider = vt.Provider
		result.Trace.VectorStatus = vt.Status
		result.Trace.VectorLatencyMillis = vt.LatencyMillis
	}
	if tracer, ok := s.reranker.(contracts.GatewayTraceProvider); ok {
		gt := tracer.LastGatewayTrace()
		result.Trace.GatewayProvider = gt.Provider
		result.Trace.GatewayStatus = gt.Status
		result.Trace.GatewayModel = gt.Model
		result.Trace.GatewayLatencyMillis = gt.LatencyMillis
		result.Trace.GatewayTokenInput = gt.TokenInput
		result.Trace.GatewayTokenOutput = gt.TokenOutput
		result.Trace.GatewayTokenTotal = gt.TokenTotal
		if result.Trace.FallbackReason == "" {
			result.Trace.FallbackReason = gt.FallbackReason
		}
	}
	if err := result.Validate(); err != nil {
		return contracts.RetrievalResult{}, contracts.WrapValidationErr("retrieval_result", err)
	}
	return result, nil
}

func (s *DeterministicRetrievalService) searchDocuments(ctx context.Context, query contracts.RetrievalQuery) ([]contracts.RetrievalDocument, string, error) {
	effectiveQuery := strings.TrimSpace(query.Text)
	rewriteApplied := false
	if s.queryRewriter != nil {
		rewritten, err := s.queryRewriter.RewriteQuery(ctx, query.TenantID, query.Text)
		if err != nil {
			logging.Logger.Warn(
				"search.step.query_rewrite.fallback",
				zap.String("tenantID", string(query.TenantID)),
				zap.String("requestID", query.RequestID),
				zap.Error(err),
			)
		} else if trimmed := strings.TrimSpace(rewritten); trimmed != "" {
			logging.Logger.Info(
				"search.step.query_rewrite.done",
				zap.String("tenantID", string(query.TenantID)),
				zap.String("requestID", query.RequestID),
				zap.String("originalQuery", query.Text),
				zap.String("rewrittenQuery", trimmed),
			)
			effectiveQuery = trimmed
			rewriteApplied = true
		}
	}
	if expanded := buildHeuristicRetrievalQuery(query.Text, effectiveQuery); expanded != effectiveQuery {
		logging.Logger.Info(
			"search.step.query_rewrite.heuristic_applied",
			zap.String("tenantID", string(query.TenantID)),
			zap.String("requestID", query.RequestID),
			zap.Bool("rewriteApplied", rewriteApplied),
			zap.String("effectiveQuery", expanded),
		)
		effectiveQuery = expanded
	}
	if embedSearch, ok := s.searcher.(vectorEmbeddingSearcher); ok && s.embedder != nil {
		vectors, err := s.embedder.Embed(ctx, query.TenantID, []string{effectiveQuery})
		if err != nil {
			return nil, effectiveQuery, fmt.Errorf("query embedding failed: %w", err)
		}
		firstDim := 0
		if len(vectors) > 0 {
			firstDim = len(vectors[0])
		}
		logging.Logger.Info(
			"search.step.embedding.response",
			zap.String("tenantID", string(query.TenantID)),
			zap.String("requestID", query.RequestID),
			zap.Int("vectorCount", len(vectors)),
			zap.Int("firstVectorDim", firstDim),
		)
		if len(vectors) != 1 || len(vectors[0]) == 0 {
			return nil, effectiveQuery, errors.New("query embedding failed: empty embedding vector")
		}
		docs, err := embedSearch.SearchByEmbedding(ctx, query.TenantID, vectors[0], query.TopK)
		return docs, effectiveQuery, err
	}
	docs, err := s.searcher.Search(ctx, query.TenantID, effectiveQuery, query.TopK)
	return docs, effectiveQuery, err
}

func buildHeuristicRetrievalQuery(originalQuery string, effectiveQuery string) string {
	base := strings.TrimSpace(effectiveQuery)
	if base == "" {
		base = strings.TrimSpace(originalQuery)
	}
	if base == "" {
		return ""
	}

	lower := strings.ToLower(originalQuery + " " + base)
	extra := make([]string, 0, 16)

	if strings.Contains(lower, "education") || strings.Contains(lower, "educational") || strings.Contains(lower, "qualification") {
		extra = append(extra,
			"academic qualification",
			"degree",
			"university",
			"college",
			"institute",
			"bachelor",
			"master",
		)
	}
	if strings.Contains(lower, "cgpa") || strings.Contains(lower, "gpa") {
		extra = append(extra,
			"cgpa",
			"gpa",
			"grade point",
			"score",
			"academic performance",
		)
	}

	identityTerms := tokenizeIdentityTerms(originalQuery)
	for _, term := range identityTerms {
		extra = append(extra, term)
	}

	if len(extra) == 0 {
		return base
	}
	seen := map[string]struct{}{}
	for _, token := range strings.Fields(strings.ToLower(base)) {
		seen[token] = struct{}{}
	}
	augmented := base
	for _, phrase := range extra {
		phrase = strings.TrimSpace(phrase)
		if phrase == "" {
			continue
		}
		phraseTokens := strings.Fields(strings.ToLower(phrase))
		alreadyCovered := true
		for _, tok := range phraseTokens {
			if _, ok := seen[tok]; !ok {
				alreadyCovered = false
				break
			}
		}
		if alreadyCovered {
			continue
		}
		augmented += " " + phrase
		for _, tok := range phraseTokens {
			seen[tok] = struct{}{}
		}
	}
	if len(augmented) > 512 {
		return augmented[:512]
	}
	return augmented
}

func (s *DeterministicRetrievalService) Rerank(ctx context.Context, metadata contracts.RequestMetadata, req contracts.RerankRequest) ([]contracts.RerankCandidate, error) {
	if err := metadata.Validate(); err != nil {
		return nil, contracts.WrapValidationErr("request_metadata", err)
	}
	if err := req.Validate(); err != nil {
		return nil, contracts.WrapValidationErr("rerank_request", err)
	}
	if metadata.TenantID != req.TenantID {
		return nil, errors.New("tenant mismatch between metadata and rerank request")
	}

	if s.reranker == nil {
		return req.TopCandidates(), nil
	}

	if s.isCircuitOpen() {
		return req.TopCandidates(), errors.New("rerank circuit breaker open")
	}

	ranked, err := s.rerankWithTimeout(ctx, req)
	if err != nil {
		s.registerFailure()
		return req.TopCandidates(), err
	}
	s.resetFailures()
	if req.TopN >= len(ranked) {
		return ranked, nil
	}
	return ranked[:req.TopN], nil
}

func (s *DeterministicRetrievalService) rerankDocuments(ctx context.Context, query contracts.RetrievalQuery, docs []contracts.RetrievalDocument) ([]contracts.RetrievalDocument, error) {
	if s.reranker == nil {
		return docs, nil
	}
	candidates := make([]contracts.RerankCandidate, 0, len(docs))
	limit := len(docs)
	if s.rerankTopK < limit {
		limit = s.rerankTopK
	}
	for i := 0; i < limit; i++ {
		doc := docs[i]
		candidates = append(candidates, contracts.RerankCandidate{DocumentID: doc.DocumentID, Text: doc.Content, Score: doc.Score})
	}
	if len(candidates) == 0 {
		return docs, nil
	}

	ranked, err := s.Rerank(ctx, contracts.RequestMetadata{TenantID: query.TenantID, RequestID: query.RequestID}, contracts.RerankRequest{
		TenantID:   query.TenantID,
		RequestID:  query.RequestID,
		QueryText:  query.Text,
		Candidates: candidates,
		TopN:       len(candidates),
	})
	if err != nil {
		return docs, err
	}

	scoreByDocument := map[string]float64{}
	for idx, candidate := range ranked {
		scoreByDocument[candidate.DocumentID] = float64(len(ranked)-idx) + candidate.Score
	}

	ordered := make([]contracts.RetrievalDocument, len(docs))
	copy(ordered, docs)
	sort.SliceStable(ordered, func(i, j int) bool {
		leftScore := scoreByDocument[ordered[i].DocumentID]
		rightScore := scoreByDocument[ordered[j].DocumentID]
		if leftScore == rightScore {
			return ordered[i].Score > ordered[j].Score
		}
		return leftScore > rightScore
	})
	return ordered, nil
}

func (s *DeterministicRetrievalService) rerankWithTimeout(ctx context.Context, req contracts.RerankRequest) ([]contracts.RerankCandidate, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, s.rerankTimeout)
	defer cancel()

	type rerankResult struct {
		candidates []contracts.RerankCandidate
		err        error
	}
	ch := make(chan rerankResult, 1)

	go func() {
		candidates, err := s.reranker.Rerank(timeoutCtx, req)
		ch <- rerankResult{candidates: candidates, err: err}
	}()

	select {
	case <-timeoutCtx.Done():
		return nil, timeoutCtx.Err()
	case out := <-ch:
		return out.candidates, out.err
	}
}

func (s *DeterministicRetrievalService) isCircuitOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consecutiveRerankFailures >= s.failureThreshold
}

func (s *DeterministicRetrievalService) registerFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutiveRerankFailures++
}

func (s *DeterministicRetrievalService) resetFailures() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutiveRerankFailures = 0
}

func buildCitations(docs []contracts.RetrievalDocument) []string {
	if len(docs) == 0 {
		return nil
	}
	citations := make([]string, 0, len(docs))
	for _, doc := range docs {
		if doc.SourceURI != "" {
			citations = append(citations, doc.SourceURI)
			continue
		}
		citations = append(citations, "doc:"+doc.DocumentID)
	}
	return citations
}

func applyMetadataIntentRanking(queryText string, docs []contracts.RetrievalDocument) []contracts.RetrievalDocument {
	if len(docs) < 2 {
		return docs
	}
	queryTerms := tokenizeIdentityTerms(queryText)
	if len(queryTerms) == 0 {
		return docs
	}

	knownIdentityTerms := map[string]struct{}{}
	for _, doc := range docs {
		for _, term := range documentIdentityTerms(doc) {
			knownIdentityTerms[term] = struct{}{}
		}
	}

	targeted := map[string]struct{}{}
	for _, term := range queryTerms {
		if _, ok := knownIdentityTerms[term]; ok {
			targeted[term] = struct{}{}
		}
	}
	if len(targeted) == 0 {
		return docs
	}

	ordered := make([]contracts.RetrievalDocument, len(docs))
	copy(ordered, docs)
	sort.SliceStable(ordered, func(i, j int) bool {
		leftMatches := countIdentityMatches(ordered[i], targeted)
		rightMatches := countIdentityMatches(ordered[j], targeted)
		if leftMatches == rightMatches {
			return ordered[i].Score > ordered[j].Score
		}
		return leftMatches > rightMatches
	})
	return ordered
}

func countIdentityMatches(doc contracts.RetrievalDocument, targeted map[string]struct{}) int {
	terms := documentIdentityTerms(doc)
	if len(terms) == 0 {
		return 0
	}
	matches := 0
	for _, term := range terms {
		if _, ok := targeted[term]; ok {
			matches++
		}
	}
	return matches
}

func documentIdentityTerms(doc contracts.RetrievalDocument) []string {
	terms := make([]string, 0, 16)
	if doc.SourceURI != "" {
		terms = append(terms, tokenizeIdentityTerms(path.Base(doc.SourceURI))...)
	}
	for _, key := range []string{"person_hint", "file_name", "relative_path", "source_uri", "object_key"} {
		if doc.Metadata != nil {
			if value := doc.Metadata[key]; strings.TrimSpace(value) != "" {
				terms = append(terms, tokenizeIdentityTerms(value)...)
			}
		}
	}
	if len(terms) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		if _, seen := set[term]; seen {
			continue
		}
		set[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

func tokenizeIdentityTerms(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	stopWords := map[string]struct{}{
		"resume": {}, "cv": {}, "pdf": {}, "skills": {}, "skill": {}, "top": {}, "show": {}, "list": {},
		"what": {}, "who": {}, "for": {}, "of": {}, "the": {}, "and": {}, "with": {}, "from": {},
		"candidate": {}, "profile": {}, "about": {}, "document": {}, "documents": {},
	}
	var b strings.Builder
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteByte(' ')
	}
	words := strings.Fields(b.String())
	out := make([]string, 0, len(words))
	for _, word := range words {
		if _, excluded := stopWords[word]; excluded {
			continue
		}
		allDigits := true
		for _, ch := range word {
			if ch < '0' || ch > '9' {
				allDigits = false
				break
			}
		}
		if allDigits || len(word) < 3 {
			continue
		}
		out = append(out, word)
	}
	return out
}
