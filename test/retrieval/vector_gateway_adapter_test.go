package retrieval_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"enterprise-go-rag/internal/adapters/gateway/portkey"
	"enterprise-go-rag/internal/adapters/vector"
	"enterprise-go-rag/internal/adapters/vector/milvus"
	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/services/retrieval"
)

func TestMilvusAdapterTenantFilterIsolation(t *testing.T) {
	adapter, err := milvus.NewAdapter(milvus.Config{
		Endpoint:   "https://milvus.example.internal",
		Database:   "db",
		Collection: "docs",
		TLS:        true,
	})
	if err != nil {
		t.Fatalf("new milvus adapter: %v", err)
	}
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	records := []contracts.VectorRecord{
		{RecordID: "a1", TenantID: "tenant-a", JobID: "job-1", ChunkText: "tenant a onboarding guide", Embedding: []float32{1}, IndexedAt: now, SourceURI: "s3://tenant-a/a.txt", Checksum: "1"},
		{RecordID: "b1", TenantID: "tenant-b", JobID: "job-2", ChunkText: "tenant b onboarding guide", Embedding: []float32{1}, IndexedAt: now, SourceURI: "s3://tenant-b/b.txt", Checksum: "2"},
	}
	if err := adapter.Upsert(t.Context(), records); err != nil {
		t.Fatalf("upsert records: %v", err)
	}

	service := retrieval.NewDeterministicRetrievalService(adapter, nil, retrieval.Config{Clock: time.Now})
	result, err := service.Search(t.Context(), contracts.RequestMetadata{TenantID: "tenant-a", RequestID: "req-1"}, contracts.RetrievalQuery{TenantID: "tenant-a", RequestID: "req-1", Text: "onboarding", TopK: 5})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(result.Documents) != 1 || result.Documents[0].TenantID != "tenant-a" {
		t.Fatalf("expected strict tenant-filtered result, got %+v", result.Documents)
	}
	if result.Trace.VectorProvider != "milvus" {
		t.Fatalf("expected vector provider trace, got %q", result.Trace.VectorProvider)
	}
}

func TestMilvusErrorTaxonomyValidationAndTimeout(t *testing.T) {
	adapter, err := milvus.NewAdapter(milvus.Config{Endpoint: "https://milvus.example.internal", Database: "db", Collection: "docs", TLS: true})
	if err != nil {
		t.Fatalf("new milvus adapter: %v", err)
	}
	_, err = adapter.Search(t.Context(), "tenant-a", "", 1)
	if err == nil {
		t.Fatal("expected validation error for empty query")
	}
	var pe contracts.ProviderError
	if !errors.As(err, &pe) || pe.Category != contracts.ProviderErrValidation {
		t.Fatalf("expected validation provider error, got %#v", err)
	}

	err = vector.NormalizeProviderError("milvus", "search", context.DeadlineExceeded)
	if !errors.As(err, &pe) || pe.Category != contracts.ProviderErrTimeout {
		t.Fatalf("expected timeout provider error, got %#v", err)
	}
}

type fakePortkeyClient struct {
	calls int
	delay time.Duration
	err   error
}

func (f *fakePortkeyClient) Rerank(ctx context.Context, req portkey.RerankRequest) ([]contracts.RerankCandidate, portkey.TokenUsage, error) {
	f.calls++
	if req.Metadata["tenant_id"] == "" || req.Metadata["request_id"] == "" {
		return nil, portkey.TokenUsage{}, errors.New("missing metadata")
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, portkey.TokenUsage{}, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, portkey.TokenUsage{}, f.err
	}
	return []contracts.RerankCandidate{{DocumentID: "doc-2", Text: "second", Score: 0.99}, {DocumentID: "doc-1", Text: "first", Score: 0.80}}, portkey.TokenUsage{Input: 10, Output: 5, Total: 15}, nil
}

func TestPortkeyMetadataPropagationAndObservability(t *testing.T) {
	adapter, err := portkey.NewRerankerAdapter(portkey.Config{
		BaseURL:                 "https://api.portkey.ai",
		APIKey:                  "pk_test_secret",
		Timeout:                 250 * time.Millisecond,
		RetryMax:                0,
		CircuitFailureThreshold: 2,
		FallbackMode:            "retrieval_only",
	})
	if err != nil {
		t.Fatalf("new portkey adapter: %v", err)
	}
	client := &fakePortkeyClient{}
	adapter.WithClient(client)

	searcher := &memorySearcher{docs: []contracts.RetrievalDocument{{DocumentID: "doc-1", TenantID: "tenant-a", Content: "first", Score: 0.8}, {DocumentID: "doc-2", TenantID: "tenant-a", Content: "second", Score: 0.7}}}
	service := retrieval.NewDeterministicRetrievalService(searcher, adapter, retrieval.Config{Clock: time.Now})
	result, err := service.Search(t.Context(), retrievalMetadata(), retrievalQuery())
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("expected one portkey call, got %d", client.calls)
	}
	meta := adapter.LastOutboundMetadata()
	if meta["tenant_id"] != "tenant-a" || meta["request_id"] == "" {
		t.Fatalf("missing propagated metadata: %+v", meta)
	}
	if result.Trace.GatewayProvider != "portkey" || result.Trace.GatewayStatus != "ok" {
		t.Fatalf("missing gateway trace fields: %+v", result.Trace)
	}
	if result.Trace.GatewayTokenTotal != 15 {
		t.Fatalf("expected token usage in trace, got %+v", result.Trace)
	}
}

func TestPortkeyTimeoutRetryAndCircuitFallback(t *testing.T) {
	adapter, err := portkey.NewRerankerAdapter(portkey.Config{
		BaseURL:                 "https://api.portkey.ai",
		APIKey:                  "pk_test_secret",
		Timeout:                 30 * time.Millisecond,
		RetryMax:                1,
		CircuitFailureThreshold: 2,
		FallbackMode:            "retrieval_only",
	})
	if err != nil {
		t.Fatalf("new portkey adapter: %v", err)
	}
	client := &fakePortkeyClient{delay: 100 * time.Millisecond}
	adapter.WithClient(client)

	req := contracts.RerankRequest{TenantID: "tenant-a", RequestID: "req-1", QueryText: "q", TopN: 2, Candidates: []contracts.RerankCandidate{{DocumentID: "doc-1", Text: "a", Score: 0.9}, {DocumentID: "doc-2", Text: "b", Score: 0.8}}}
	for i := 0; i < 3; i++ {
		out, err := adapter.Rerank(t.Context(), req)
		if err != nil {
			t.Fatalf("retrieval_only fallback should not return error, got: %v", err)
		}
		if len(out) != 2 {
			t.Fatalf("expected fallback candidates, got %d", len(out))
		}
	}
	if client.calls != 4 {
		t.Fatalf("expected retries before circuit open, got calls=%d", client.calls)
	}
	trace := adapter.LastGatewayTrace()
	if trace.FallbackReason == "" {
		t.Fatalf("expected fallback reason in gateway trace: %+v", trace)
	}
}

func TestPortkeyFailClosedAndSecretRedaction(t *testing.T) {
	adapter, err := portkey.NewRerankerAdapter(portkey.Config{
		BaseURL:                 "https://api.portkey.ai",
		APIKey:                  "pk_live_supersecret",
		Timeout:                 20 * time.Millisecond,
		RetryMax:                0,
		CircuitFailureThreshold: 1,
		FallbackMode:            "fail_closed",
	})
	if err != nil {
		t.Fatalf("new portkey adapter: %v", err)
	}
	adapter.WithClient(&fakePortkeyClient{err: errors.New("unauthorized token=pk_live_supersecret")})
	req := contracts.RerankRequest{TenantID: "tenant-a", RequestID: "req-1", QueryText: "q", TopN: 1, Candidates: []contracts.RerankCandidate{{DocumentID: "doc-1", Text: "a", Score: 0.9}}}
	_, err = adapter.Rerank(t.Context(), req)
	if err == nil {
		t.Fatal("expected fail_closed to return error")
	}
	if strings.Contains(err.Error(), "pk_live_supersecret") {
		t.Fatalf("secret leaked in error string: %v", err)
	}
	if adapter.RedactedAPIKey() != "REDACTED" {
		t.Fatal("expected redacted api key")
	}

	attrs := portkey.BuildAuditAttributes(req, adapter.LastGatewayTrace())
	if attrs["tenant_id"] != "tenant-a" || attrs["request_id"] == "" || attrs["fallback_reason"] == "" {
		t.Fatalf("expected audit rationale attributes, got %+v", attrs)
	}
}
