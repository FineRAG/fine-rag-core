package repository

import (
	"context"
	"errors"
	"testing"

	"enterprise-go-rag/internal/contracts"
)

func TestIsolationGuardReadScopeRejectsUnscopedContext(t *testing.T) {
	if err := GuardReadScope(context.Background(), "tenant-a"); err == nil {
		t.Fatal("expected unscoped read to be rejected")
	} else if !errors.Is(err, ErrUnscopedRepositoryAccess) {
		t.Fatalf("expected ErrUnscopedRepositoryAccess, got: %v", err)
	}
}

func TestIsolationGuardWriteScopeRejectsCrossTenantWrite(t *testing.T) {
	ctx, err := contracts.WithTenantContext(t.Context(), contracts.TenantContext{TenantID: "tenant-a", RequestID: "req-1"})
	if err != nil {
		t.Fatalf("seed tenant context: %v", err)
	}

	if err := GuardWriteScope(ctx, "tenant-b"); err == nil {
		t.Fatal("expected cross-tenant write to be rejected")
	}
}

func TestIsolationGuardReadScopeAllowsMatchingTenant(t *testing.T) {
	ctx, err := contracts.WithTenantContext(t.Context(), contracts.TenantContext{TenantID: "tenant-a", RequestID: "req-1"})
	if err != nil {
		t.Fatalf("seed tenant context: %v", err)
	}

	if err := GuardReadScope(ctx, "tenant-a"); err != nil {
		t.Fatalf("expected matching tenant read to succeed, got: %v", err)
	}
}
