package retrieval

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"enterprise-go-rag/internal/contracts"
)

// Service defines retrieval boundaries for query execution and reranking.
type Service interface {
	Search(ctx context.Context, metadata contracts.RequestMetadata, query contracts.RetrievalQuery) (contracts.RetrievalResult, error)
	Rerank(ctx context.Context, metadata contracts.RequestMetadata, req contracts.RerankRequest) ([]contracts.RerankCandidate, error)
}

type Config struct {
	RerankTopK       int
	RerankTimeout    time.Duration
	FailureThreshold int
	Clock            func() time.Time
}

type DeterministicRetrievalService struct {
	searcher contracts.VectorSearcher
	reranker contracts.Reranker
	clock    func() time.Time

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

	docs, err := s.searcher.Search(ctx, query.TenantID, query.Text, query.TopK)
	if err != nil {
		return contracts.RetrievalResult{}, err
	}

	filtered := make([]contracts.RetrievalDocument, 0, len(docs))
	for _, doc := range docs {
		if doc.TenantID == query.TenantID {
			filtered = append(filtered, doc)
		}
	}

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
	if err := result.Validate(); err != nil {
		return contracts.RetrievalResult{}, contracts.WrapValidationErr("retrieval_result", err)
	}
	return result, nil
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
