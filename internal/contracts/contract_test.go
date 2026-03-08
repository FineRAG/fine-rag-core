package contracts

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubAuthorizer struct{}

func (stubAuthorizer) Authorize(_ context.Context, claims AuthClaims, metadata RequestMetadata) error {
	if err := claims.Validate(); err != nil {
		return err
	}
	return metadata.Validate()
}

type stubRateLimiter struct{}

func (stubRateLimiter) Allow(_ context.Context, tenantID TenantID, _ string) (RateLimitDecision, error) {
	if err := tenantID.Validate(); err != nil {
		return RateLimitDecision{}, err
	}
	return RateLimitDecision{Allowed: true, Remaining: 1}, nil
}

func TestContractTenantIDMandatoryAcrossCoreContracts(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "tenant context", err: (TenantContext{RequestID: "req-1"}).Validate()},
		{name: "auth claims", err: (AuthClaims{Subject: "svc", APIKeyID: "key", IssuedAt: time.Now()}).Validate()},
		{name: "request metadata", err: (RequestMetadata{RequestID: "req-1"}).Validate()},
		{name: "ingestion job", err: (IngestionJob{JobID: "job", SourceURI: "s3://x", CreatedAt: time.Now()}).Validate()},
		{name: "retrieval query", err: (RetrievalQuery{RequestID: "req-1", Text: "hello", TopK: 5}).Validate()},
		{name: "rerank request", err: (RerankRequest{RequestID: "req-1", QueryText: "q", Candidates: []RerankCandidate{{DocumentID: "d1", Text: "t"}}, TopN: 1}).Validate()},
		{name: "audit event", err: (AuditEvent{EventID: "evt", EventType: "auth", OccurredAt: time.Now()}).Validate()},
	}

	for _, tc := range tests {
		if tc.err == nil {
			t.Fatalf("expected tenant validation error for %s", tc.name)
		}
	}
}

func TestContractExtensionPointsAreTenantAware(t *testing.T) {
	authz := stubAuthorizer{}
	limiter := stubRateLimiter{}

	if err := authz.Authorize(
		context.Background(),
		AuthClaims{TenantID: "t-1", Subject: "svc", APIKeyID: "key-1", IssuedAt: time.Now()},
		RequestMetadata{TenantID: "t-1", RequestID: "req-1"},
	); err != nil {
		t.Fatalf("expected authorizer to accept valid tenant-aware input, got: %v", err)
	}

	if _, err := limiter.Allow(context.Background(), "", "search"); err == nil {
		t.Fatal("expected rate limiter to reject empty tenant id")
	}
}

func TestTenantContextPropagationRoundTrip(t *testing.T) {
	tenantCtx := TenantContext{TenantID: "tenant-a", RequestID: "req-1"}

	ctx, err := WithTenantContext(context.Background(), tenantCtx)
	if err != nil {
		t.Fatalf("expected tenant context insertion to succeed, got: %v", err)
	}

	restored, err := TenantContextFromContext(ctx)
	if err != nil {
		t.Fatalf("expected tenant context extraction to succeed, got: %v", err)
	}

	if restored != tenantCtx {
		t.Fatalf("tenant context mismatch: got %#v want %#v", restored, tenantCtx)
	}
}

func TestTenantContextPropagationImmutableByValue(t *testing.T) {
	tenantCtx := TenantContext{TenantID: "tenant-a", RequestID: "req-1"}
	ctx, err := WithTenantContext(context.Background(), tenantCtx)
	if err != nil {
		t.Fatalf("insert tenant context: %v", err)
	}

	restored, err := TenantContextFromContext(ctx)
	if err != nil {
		t.Fatalf("extract tenant context: %v", err)
	}

	restored.TenantID = "tenant-b"

	again, err := TenantContextFromContext(ctx)
	if err != nil {
		t.Fatalf("extract tenant context second read: %v", err)
	}

	if again.TenantID != "tenant-a" {
		t.Fatalf("tenant context mutated in-place: got %s", again.TenantID)
	}
}

func TestTenantContextMalformedRejected(t *testing.T) {
	ctx, err := WithTenantContext(context.Background(), TenantContext{TenantID: "", RequestID: "req-1"})
	if err == nil {
		t.Fatal("expected malformed tenant context insertion to fail")
	}
	if !errors.Is(err, ErrTenantContextMalformed) {
		t.Fatalf("expected ErrTenantContextMalformed, got: %v", err)
	}
	if ctx != nil {
		t.Fatal("expected no context returned on malformed tenant input")
	}
}

func TestIsolationRequireTenantScope(t *testing.T) {
	if _, err := RequireTenantScope(context.Background()); err == nil {
		t.Fatal("expected missing tenant scope error")
	}
}

func TestIsolationEnsureTenantMatch(t *testing.T) {
	ctx, err := WithTenantContext(context.Background(), TenantContext{TenantID: "tenant-a", RequestID: "req-1"})
	if err != nil {
		t.Fatalf("insert tenant context: %v", err)
	}

	if err := EnsureTenantMatch(ctx, "tenant-b"); err == nil {
		t.Fatal("expected tenant mismatch error")
	}
}
