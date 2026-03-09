package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"enterprise-go-rag/internal/contracts"
)

const (
	DefaultTenantSteadyRPS = 2
	DefaultTenantBurstRPS  = 4
	DefaultGlobalBurstRPS  = 600
)

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type windowCounter struct {
	windowEpochSecond int64
	count             int
}

type Limiter struct {
	mu sync.Mutex

	clock Clock

	TenantSteadyRPS int
	TenantBurstRPS  int
	GlobalBurstRPS  int

	tenant map[contracts.TenantID]windowCounter
	global windowCounter
}

func NewLimiter() *Limiter {
	return &Limiter{
		clock:           systemClock{},
		TenantSteadyRPS: DefaultTenantSteadyRPS,
		TenantBurstRPS:  DefaultTenantBurstRPS,
		GlobalBurstRPS:  DefaultGlobalBurstRPS,
		tenant:          make(map[contracts.TenantID]windowCounter),
	}
}

func (l *Limiter) Allow(_ context.Context, tenantID contracts.TenantID, _ string) (contracts.RateLimitDecision, error) {
	if l == nil {
		return contracts.RateLimitDecision{}, errors.New("rate limiter is required")
	}
	if err := tenantID.Validate(); err != nil {
		return contracts.RateLimitDecision{}, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	nowSec := l.clock.Now().Unix()

	l.global = nextWindow(l.global, nowSec)
	if l.global.count >= l.GlobalBurstRPS {
		decision := contracts.RateLimitDecision{Allowed: false, Reason: "global_burst_limit_exceeded", Remaining: 0, RetryAfter: time.Second}
		return decision, decision.Validate()
	}

	tenantCounter := nextWindow(l.tenant[tenantID], nowSec)
	if tenantCounter.count >= l.TenantBurstRPS {
		decision := contracts.RateLimitDecision{Allowed: false, Reason: fmt.Sprintf("tenant_burst_limit_exceeded:%s", tenantID), Remaining: 0, RetryAfter: time.Second}
		return decision, decision.Validate()
	}

	tenantCounter.count++
	l.tenant[tenantID] = tenantCounter
	l.global.count++

	remaining := l.TenantBurstRPS - tenantCounter.count
	return contracts.RateLimitDecision{Allowed: true, Remaining: remaining}, nil
}

func nextWindow(counter windowCounter, nowSec int64) windowCounter {
	if counter.windowEpochSecond != nowSec {
		return windowCounter{windowEpochSecond: nowSec, count: 0}
	}
	return counter
}

func (l *Limiter) SetClock(clock Clock) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clock = clock
}
