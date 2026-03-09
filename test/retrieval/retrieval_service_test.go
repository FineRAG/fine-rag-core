package retrieval_test

import (
	"context"
	"errors"
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
