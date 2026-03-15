package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"

	"enterprise-go-rag/internal/contracts"
)

const (
	AuthenticatedTenantIDHeader = "X-Authenticated-Tenant-ID"
	RequestIDHeader             = "X-Request-ID"
)

type TenantContextRejection struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type TenantObservabilityMetadata struct {
	RequestID   string
	TenantLabel string
}

func TenantContextMiddleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantRaw := strings.TrimSpace(r.Header.Get(AuthenticatedTenantIDHeader))
		if tenantRaw == "" {
			writeTenantContextRejection(w, http.StatusUnauthorized, "tenant_context_missing", "authenticated tenant context is required")
			return
		}

		requestID := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if requestID == "" {
			writeTenantContextRejection(w, http.StatusBadRequest, "request_id_missing", "request id is required")
			return
		}

		tenantContext := contracts.TenantContext{TenantID: contracts.TenantID(tenantRaw), RequestID: requestID}
		ctx, err := contracts.WithTenantContext(r.Context(), tenantContext)
		if err != nil {
			writeTenantContextRejection(w, http.StatusBadRequest, "tenant_context_malformed", "tenant context is malformed")
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func ObservabilityMetadataFromContext(ctx context.Context) (TenantObservabilityMetadata, error) {
	tenantContext, err := contracts.TenantContextFromContext(ctx)
	if err != nil {
		return TenantObservabilityMetadata{}, err
	}

	return TenantObservabilityMetadata{
		RequestID:   tenantContext.RequestID,
		TenantLabel: tenantLabel(tenantContext.TenantID),
	}, nil
}

func tenantLabel(tenantID contracts.TenantID) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(tenantID))
	return fmt.Sprintf("tenant_%08x", h.Sum32())
}

func writeTenantContextRejection(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	payload := TenantContextRejection{}
	payload.Error.Code = code
	payload.Error.Message = message

	_ = json.NewEncoder(w).Encode(payload)
}
