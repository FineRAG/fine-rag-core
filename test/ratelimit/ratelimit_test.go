package ratelimit_test

import (
	"testing"
	"time"

	"enterprise-go-rag/internal/ratelimit"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

func TestRateLimitEnforcesTenantQuotaAndBurst(t *testing.T) {
	l := ratelimit.NewLimiter()
	fc := &fakeClock{now: time.Unix(100, 0).UTC()}
	l.SetClock(fc)

	for i := 0; i < ratelimit.DefaultTenantBurstRPS; i++ {
		decision, err := l.Allow(t.Context(), "tenant-a", "search")
		if err != nil {
			t.Fatalf("allow call %d unexpected error: %v", i, err)
		}
		if !decision.Allowed {
			t.Fatalf("expected allowed at i=%d, got denied reason=%s", i, decision.Reason)
		}
	}

	decision, err := l.Allow(t.Context(), "tenant-a", "search")
	if err != nil {
		t.Fatalf("expected rate-limit decision error to be nil, got: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected tenant burst denial after quota exhaustion")
	}
}

func TestQuotaIsolationDoesNotLeakAcrossTenants(t *testing.T) {
	l := ratelimit.NewLimiter()
	fc := &fakeClock{now: time.Unix(100, 0).UTC()}
	l.SetClock(fc)

	for i := 0; i < ratelimit.DefaultTenantBurstRPS; i++ {
		_, _ = l.Allow(t.Context(), "tenant-a", "search")
	}

	decision, err := l.Allow(t.Context(), "tenant-b", "search")
	if err != nil {
		t.Fatalf("unexpected error for tenant-b: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected tenant-b to remain allowed, got denied reason=%s", decision.Reason)
	}
}
