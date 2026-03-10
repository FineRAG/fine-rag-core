package retrieval_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/services/retrieval"
)

type memorySearcher struct {
	docs []contracts.RetrievalDocument
}

func (m *memorySearcher) Search(_ context.Context, _ contracts.TenantID, _ string, _ int) ([]contracts.RetrievalDocument, error) {
	copied := make([]contracts.RetrievalDocument, len(m.docs))
	copy(copied, m.docs)
	return copied, nil
}

type embeddingAwareSearcher struct {
	docs            []contracts.RetrievalDocument
	lastLexicalText string
	embeddingCalls  int
}

func (m *embeddingAwareSearcher) Search(_ context.Context, _ contracts.TenantID, queryText string, _ int) ([]contracts.RetrievalDocument, error) {
	m.lastLexicalText = queryText
	copied := make([]contracts.RetrievalDocument, len(m.docs))
	copy(copied, m.docs)
	return copied, nil
}

func (m *embeddingAwareSearcher) SearchByEmbedding(_ context.Context, _ contracts.TenantID, _ []float32, _ int) ([]contracts.RetrievalDocument, error) {
	m.embeddingCalls++
	copied := make([]contracts.RetrievalDocument, len(m.docs))
	copy(copied, m.docs)
	return copied, nil
}

type captureEmbedder struct {
	lastChunks []string
}

func (c *captureEmbedder) Embed(_ context.Context, _ contracts.TenantID, chunks []string) ([][]float32, error) {
	c.lastChunks = append([]string(nil), chunks...)
	return [][]float32{{0.12, 0.34, 0.56}}, nil
}

type staticQueryRewriter struct {
	rewritten string
}

func (s staticQueryRewriter) RewriteQuery(_ context.Context, _ contracts.TenantID, _ string) (string, error) {
	return s.rewritten, nil
}

type failingQueryRewriter struct{}

func (f failingQueryRewriter) RewriteQuery(_ context.Context, _ contracts.TenantID, _ string) (string, error) {
	return "", errors.New("empty query rewrite response")
}

type deterministicReranker struct {
	orderedIDs []string
	delay      time.Duration
	calls      int
}

func (d *deterministicReranker) Rerank(ctx context.Context, req contracts.RerankRequest) ([]contracts.RerankCandidate, error) {
	d.calls++
	if d.delay > 0 {
		select {
		case <-time.After(d.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	candidates := req.TopCandidates()
	if len(d.orderedIDs) == 0 {
		return candidates, nil
	}
	candidateByID := map[string]contracts.RerankCandidate{}
	for _, c := range candidates {
		candidateByID[c.DocumentID] = c
	}
	ordered := make([]contracts.RerankCandidate, 0, len(candidates))
	for _, id := range d.orderedIDs {
		if c, ok := candidateByID[id]; ok {
			ordered = append(ordered, c)
			delete(candidateByID, id)
		}
	}
	for _, c := range candidates {
		if _, ok := candidateByID[c.DocumentID]; ok {
			ordered = append(ordered, c)
		}
	}
	return ordered, nil
}

type failingReranker struct {
	calls int
}

func (f *failingReranker) Rerank(_ context.Context, _ contracts.RerankRequest) ([]contracts.RerankCandidate, error) {
	f.calls++
	return nil, errors.New("reranker unavailable")
}

func retrievalMetadata() contracts.RequestMetadata {
	return contracts.RequestMetadata{TenantID: "tenant-a", RequestID: "req-r-1"}
}

func retrievalQuery() contracts.RetrievalQuery {
	return contracts.RetrievalQuery{TenantID: "tenant-a", RequestID: "req-r-1", Text: "how to onboard", TopK: 3}
}

func TestRetrievalTenantFilterAppliedAndCitationsIncluded(t *testing.T) {
	searcher := &memorySearcher{docs: []contracts.RetrievalDocument{
		{DocumentID: "doc-a", TenantID: "tenant-a", Content: "A", Score: 0.90, SourceURI: "s3://tenant-a/docs/a.txt"},
		{DocumentID: "doc-b", TenantID: "tenant-b", Content: "B", Score: 0.99, SourceURI: "s3://tenant-b/docs/b.txt"},
		{DocumentID: "doc-c", TenantID: "tenant-a", Content: "C", Score: 0.70, SourceURI: "s3://tenant-a/docs/c.txt"},
	}}
	service := retrieval.NewDeterministicRetrievalService(searcher, nil, retrieval.Config{Clock: time.Now})

	result, err := service.Search(t.Context(), retrievalMetadata(), retrievalQuery())
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(result.Documents) != 2 {
		t.Fatalf("expected only tenant-a docs, got %d", len(result.Documents))
	}
	for _, doc := range result.Documents {
		if doc.TenantID != "tenant-a" {
			t.Fatalf("unexpected tenant leakage: %+v", doc)
		}
	}
	if !result.Trace.TenantFilterApplied {
		t.Fatal("expected tenant filter trace flag")
	}
	if len(result.Citations) != 2 {
		t.Fatalf("expected citations for all returned docs, got %d", len(result.Citations))
	}
}

func TestRerankFallbackWhenTimeout(t *testing.T) {
	searcher := &memorySearcher{docs: []contracts.RetrievalDocument{
		{DocumentID: "doc-a", TenantID: "tenant-a", Content: "A", Score: 0.91},
		{DocumentID: "doc-c", TenantID: "tenant-a", Content: "C", Score: 0.89},
	}}
	reranker := &deterministicReranker{orderedIDs: []string{"doc-c", "doc-a"}, delay: 300 * time.Millisecond}
	service := retrieval.NewDeterministicRetrievalService(searcher, reranker, retrieval.Config{RerankTimeout: 50 * time.Millisecond})

	result, err := service.Search(t.Context(), retrievalMetadata(), retrievalQuery())
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if result.Trace.RerankApplied {
		t.Fatal("expected rerank to be skipped due to timeout fallback")
	}
	if result.Trace.FallbackReason == "" {
		t.Fatal("expected fallback reason for timeout")
	}
	if result.Documents[0].DocumentID != "doc-a" {
		t.Fatalf("expected original order preserved on fallback, got first=%s", result.Documents[0].DocumentID)
	}
}

func TestRerankCircuitBreakerFallback(t *testing.T) {
	searcher := &memorySearcher{docs: []contracts.RetrievalDocument{
		{DocumentID: "doc-a", TenantID: "tenant-a", Content: "A", Score: 0.91},
		{DocumentID: "doc-c", TenantID: "tenant-a", Content: "C", Score: 0.89},
	}}
	reranker := &failingReranker{}
	service := retrieval.NewDeterministicRetrievalService(searcher, reranker, retrieval.Config{FailureThreshold: 2})

	_, err := service.Search(t.Context(), retrievalMetadata(), retrievalQuery())
	if err != nil {
		t.Fatalf("search first attempt failed: %v", err)
	}
	_, err = service.Search(t.Context(), retrievalMetadata(), retrievalQuery())
	if err != nil {
		t.Fatalf("search second attempt failed: %v", err)
	}
	if reranker.calls != 2 {
		t.Fatalf("expected reranker to be called twice before circuit opens, got %d", reranker.calls)
	}

	result, err := service.Search(t.Context(), retrievalMetadata(), retrievalQuery())
	if err != nil {
		t.Fatalf("search third attempt failed: %v", err)
	}
	if reranker.calls != 2 {
		t.Fatalf("expected circuit breaker to prevent additional rerank calls, got %d", reranker.calls)
	}
	if result.Trace.FallbackReason == "" {
		t.Fatal("expected circuit-breaker fallback reason")
	}
}

func TestRetrievalPrefersMetadataIdentityMatch(t *testing.T) {
	searcher := &memorySearcher{docs: []contracts.RetrievalDocument{
		{
			DocumentID: "doc-rounak",
			TenantID:   "tenant-a",
			Content:    "Rounak has strong Java and distributed systems experience",
			Score:      0.95,
			SourceURI:  "local://RounakPoddar-Resume.pdf",
			Metadata:   map[string]string{"person_hint": "rounak poddar", "file_name": "RounakPoddar-Resume.pdf"},
		},
		{
			DocumentID: "doc-shafeeq",
			TenantID:   "tenant-a",
			Content:    "Shafeeq has Rust and AWS microservice architecture experience",
			Score:      0.91,
			SourceURI:  "local://Shafeeq-Resume-Mar-2026.pdf",
			Metadata:   map[string]string{"person_hint": "shafeeq", "file_name": "Shafeeq-Resume-Mar-2026.pdf"},
		},
	}}
	service := retrieval.NewDeterministicRetrievalService(searcher, nil, retrieval.Config{Clock: time.Now})

	result, err := service.Search(t.Context(), retrievalMetadata(), contracts.RetrievalQuery{
		TenantID:  "tenant-a",
		RequestID: "req-r-2",
		Text:      "Top skills of Shafeeq",
		TopK:      5,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(result.Documents) < 2 {
		t.Fatalf("expected two docs, got %d", len(result.Documents))
	}
	if result.Documents[0].DocumentID != "doc-shafeeq" {
		t.Fatalf("expected shafeeq doc first, got %s", result.Documents[0].DocumentID)
	}
}

func TestRetrievalUsesRewrittenQueryForEmbedding(t *testing.T) {
	searcher := &embeddingAwareSearcher{docs: []contracts.RetrievalDocument{
		{DocumentID: "doc-1", TenantID: "tenant-a", Content: "Education: IIT Delhi", Score: 0.88, SourceURI: "local://RounakPoddar-Resume.pdf"},
	}}
	embedder := &captureEmbedder{}
	service := retrieval.NewDeterministicRetrievalService(searcher, nil, retrieval.Config{
		Clock:             time.Now,
		EmbeddingProvider: embedder,
		QueryRewriter:     staticQueryRewriter{rewritten: "rounak poddar education qualifications degree institute"},
	})

	_, err := service.Search(t.Context(), retrievalMetadata(), contracts.RetrievalQuery{
		TenantID:  "tenant-a",
		RequestID: "req-r-3",
		Text:      "Tell me about Rounak's educational qualification",
		TopK:      5,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(embedder.lastChunks) != 1 {
		t.Fatalf("expected one embedded query chunk, got %d", len(embedder.lastChunks))
	}
	if got := embedder.lastChunks[0]; !strings.Contains(strings.ToLower(got), "rounak poddar education qualifications degree institute") {
		t.Fatalf("expected rewritten query core text to be embedded, got %q", got)
	}
	if searcher.embeddingCalls != 1 {
		t.Fatalf("expected embedding search call, got %d", searcher.embeddingCalls)
	}
}

func TestRetrievalExpandsEducationQueryWhenRewriteFails(t *testing.T) {
	searcher := &embeddingAwareSearcher{docs: []contracts.RetrievalDocument{
		{DocumentID: "doc-1", TenantID: "tenant-a", Content: "Education: IIT Delhi, CGPA 8.9", Score: 0.88, SourceURI: "local://Shafeeq-Resume.pdf"},
	}}
	embedder := &captureEmbedder{}
	service := retrieval.NewDeterministicRetrievalService(searcher, nil, retrieval.Config{
		Clock:             time.Now,
		EmbeddingProvider: embedder,
		QueryRewriter:     failingQueryRewriter{},
	})

	_, err := service.Search(t.Context(), retrievalMetadata(), contracts.RetrievalQuery{
		TenantID:  "tenant-a",
		RequestID: "req-r-4",
		Text:      "What is Shafeeq education qualification and CGPA?",
		TopK:      5,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(embedder.lastChunks) != 1 {
		t.Fatalf("expected one embedded query chunk, got %d", len(embedder.lastChunks))
	}
	got := embedder.lastChunks[0]
	for _, expected := range []string{"shafeeq", "education", "qualification", "cgpa", "gpa", "degree", "university"} {
		if !containsToken(got, expected) {
			t.Fatalf("expected expanded query to include %q, got %q", expected, got)
		}
	}
	if searcher.embeddingCalls != 1 {
		t.Fatalf("expected embedding search call, got %d", searcher.embeddingCalls)
	}
}

func containsToken(input string, token string) bool {
	for _, part := range strings.Fields(strings.ToLower(input)) {
		if part == token {
			return true
		}
	}
	return false
}
