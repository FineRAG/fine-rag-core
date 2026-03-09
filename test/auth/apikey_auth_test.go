package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"enterprise-go-rag/internal/auth"
	"enterprise-go-rag/internal/contracts"
)

type memoryAuditSink struct {
	events []contracts.AuditEvent
}

func (m *memoryAuditSink) Write(_ context.Context, event contracts.AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

func seedStore() (*auth.InMemoryAPIKeyStore, string, string) {
	store := auth.NewInMemoryAPIKeyStore()
	validRaw := "key-valid"
	revokedRaw := "key-revoked"

	store.Put(auth.APIKeyRecord{
		APIKeyID:      "k1",
		TenantID:      "tenant-a",
		Subject:       "svc-a",
		RawKeySHA256:  auth.HashAPIKey(validRaw),
		IssuedAt:      time.Now().UTC(),
		AllowedScopes: []string{"search"},
	})

	store.Put(auth.APIKeyRecord{
		APIKeyID:     "k2",
		TenantID:     "tenant-a",
		Subject:      "svc-a",
		RawKeySHA256: auth.HashAPIKey(revokedRaw),
		Revoked:      true,
		RevokedAt:    time.Now().UTC(),
		IssuedAt:     time.Now().UTC(),
	})

	return store, validRaw, revokedRaw
}

func TestAPIKeyAuthDeniesInvalidAndRevokedKeys(t *testing.T) {
	store, _, revokedRaw := seedStore()
	audit := &memoryAuditSink{}
	a := auth.APIKeyAuthenticator{Store: store, AuditSink: audit}
	meta := contracts.RequestMetadata{TenantID: "tenant-a", RequestID: "req-1"}

	if _, err := a.Authenticate(t.Context(), "not-found", meta); !errors.Is(err, auth.ErrAPIKeyInvalid) {
		t.Fatalf("expected ErrAPIKeyInvalid, got %v", err)
	}
	if _, err := a.Authenticate(t.Context(), revokedRaw, meta); !errors.Is(err, auth.ErrAPIKeyRevoked) {
		t.Fatalf("expected ErrAPIKeyRevoked, got %v", err)
	}
}

func TestAuthAuditContainsTenantAndRequestIdentifiers(t *testing.T) {
	store, validRaw, _ := seedStore()
	audit := &memoryAuditSink{}
	a := auth.APIKeyAuthenticator{Store: store, AuditSink: audit}
	meta := contracts.RequestMetadata{TenantID: "tenant-a", RequestID: "req-2"}

	if _, err := a.Authenticate(t.Context(), validRaw, meta); err != nil {
		t.Fatalf("authenticate valid key: %v", err)
	}

	if len(audit.events) == 0 {
		t.Fatal("expected at least one audit event")
	}

	last := audit.events[len(audit.events)-1]
	if last.TenantID != "tenant-a" {
		t.Fatalf("unexpected tenant id in audit event: %s", last.TenantID)
	}
	if last.Actor != "req-2" {
		t.Fatalf("unexpected request id actor in audit event: %s", last.Actor)
	}
}
