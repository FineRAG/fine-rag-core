package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/middleware"
)

func TestTenantContextMiddlewareRejectsMissingTenant(t *testing.T) {
	h := middleware.TenantContextMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not execute when tenant context is missing")
	}))

	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	req.Header.Set(middleware.RequestIDHeader, "req-1")
	res := httptest.NewRecorder()

	h.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}

	var body middleware.TenantContextRejection
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode rejection body: %v", err)
	}
	if body.Error.Code != "tenant_context_missing" {
		t.Fatalf("unexpected rejection code: %s", body.Error.Code)
	}
}

func TestTenantContextMiddlewareRejectsMissingRequestID(t *testing.T) {
	h := middleware.TenantContextMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not execute when request id is missing")
	}))

	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	req.Header.Set(middleware.AuthenticatedTenantIDHeader, "tenant-a")
	res := httptest.NewRecorder()

	h.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.Code)
	}
}

func TestTenantContextMiddlewareInjectsTenantContext(t *testing.T) {
	called := false
	h := middleware.TenantContextMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		tenantCtx, err := contracts.TenantContextFromContext(r.Context())
		if err != nil {
			t.Fatalf("expected tenant context in request context, got: %v", err)
		}
		if tenantCtx.TenantID != "tenant-a" {
			t.Fatalf("unexpected tenant id: %s", tenantCtx.TenantID)
		}
		if tenantCtx.RequestID != "req-1" {
			t.Fatalf("unexpected request id: %s", tenantCtx.RequestID)
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	req.Header.Set(middleware.AuthenticatedTenantIDHeader, "tenant-a")
	req.Header.Set(middleware.RequestIDHeader, "req-1")
	res := httptest.NewRecorder()

	h.ServeHTTP(res, req)

	if !called {
		t.Fatal("expected downstream handler to execute")
	}
}

func TestTenantContextObservabilityMetadataRedactsTenantID(t *testing.T) {
	ctx, err := contracts.WithTenantContext(
		httptest.NewRequest(http.MethodGet, "/", nil).Context(),
		contracts.TenantContext{TenantID: "tenant-secret", RequestID: "req-9"},
	)
	if err != nil {
		t.Fatalf("seed tenant context: %v", err)
	}

	meta, err := middleware.ObservabilityMetadataFromContext(ctx)
	if err != nil {
		t.Fatalf("extract observability metadata: %v", err)
	}

	if meta.RequestID != "req-9" {
		t.Fatalf("unexpected request id: %s", meta.RequestID)
	}
	if meta.TenantLabel == "" {
		t.Fatal("expected tenant label")
	}
	if meta.TenantLabel == "tenant-secret" {
		t.Fatal("tenant label should not expose raw tenant id")
	}
}
