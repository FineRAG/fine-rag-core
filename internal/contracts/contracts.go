package contracts

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TenantID identifies a tenant and is mandatory across request and persistence contracts.
type TenantID string

func (t TenantID) Validate() error {
	if t == "" {
		return errors.New("tenant_id is required")
	}
	return nil
}

type TenantContext struct {
	TenantID  TenantID
	RequestID string
}

var (
	ErrTenantContextMissing   = errors.New("tenant context missing")
	ErrTenantContextMalformed = errors.New("tenant context malformed")
	ErrTenantScopeRequired    = errors.New("tenant scope is required")
	ErrTenantScopeMismatch    = errors.New("tenant scope mismatch")
)

type tenantContextKey struct{}

func (c TenantContext) Validate() error {
	if err := c.TenantID.Validate(); err != nil {
		return err
	}
	if c.RequestID == "" {
		return errors.New("request_id is required")
	}
	return nil
}

func WithTenantContext(ctx context.Context, tenantContext TenantContext) (context.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := tenantContext.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTenantContextMalformed, err)
	}

	return context.WithValue(ctx, tenantContextKey{}, tenantContext), nil
}

func TenantContextFromContext(ctx context.Context) (TenantContext, error) {
	if ctx == nil {
		return TenantContext{}, ErrTenantContextMissing
	}

	raw := ctx.Value(tenantContextKey{})
	if raw == nil {
		return TenantContext{}, ErrTenantContextMissing
	}

	tenantContext, ok := raw.(TenantContext)
	if !ok {
		return TenantContext{}, ErrTenantContextMalformed
	}

	if err := tenantContext.Validate(); err != nil {
		return TenantContext{}, fmt.Errorf("%w: %v", ErrTenantContextMalformed, err)
	}

	return tenantContext, nil
}

func RequireTenantScope(ctx context.Context) (TenantContext, error) {
	tenantContext, err := TenantContextFromContext(ctx)
	if err != nil {
		return TenantContext{}, fmt.Errorf("%w: %v", ErrTenantScopeRequired, err)
	}
	return tenantContext, nil
}

func EnsureTenantMatch(ctx context.Context, tenantID TenantID) error {
	tenantContext, err := RequireTenantScope(ctx)
	if err != nil {
		return err
	}

	if err := tenantID.Validate(); err != nil {
		return err
	}

	if tenantContext.TenantID != tenantID {
		return fmt.Errorf("%w: scope=%s target=%s", ErrTenantScopeMismatch, tenantContext.TenantID, tenantID)
	}

	return nil
}

type AuthClaims struct {
	TenantID TenantID
	Subject  string
	APIKeyID string
	Scopes   []string
	IssuedAt time.Time
}

func (c AuthClaims) Validate() error {
	if err := c.TenantID.Validate(); err != nil {
		return err
	}
	if c.Subject == "" {
		return errors.New("subject is required")
	}
	if c.APIKeyID == "" {
		return errors.New("api_key_id is required")
	}
	if c.IssuedAt.IsZero() {
		return errors.New("issued_at is required")
	}
	return nil
}

type RequestMetadata struct {
	TenantID  TenantID
	RequestID string
	SourceIP  string
	UserAgent string
}

func (m RequestMetadata) Validate() error {
	if err := m.TenantID.Validate(); err != nil {
		return err
	}
	if m.RequestID == "" {
		return errors.New("request_id is required")
	}
	return nil
}

type IngestionStatus string

const (
	IngestionStatusQueued     IngestionStatus = "queued"
	IngestionStatusApproved   IngestionStatus = "approved"
	IngestionStatusQuarantine IngestionStatus = "quarantine"
	IngestionStatusRejected   IngestionStatus = "rejected"
	IngestionStatusIndexing   IngestionStatus = "indexing"
	IngestionStatusIndexed    IngestionStatus = "indexed"
	IngestionStatusFailed     IngestionStatus = "failed"
)

type IngestionJob struct {
	JobID          string
	TenantID       TenantID
	SourceURI      string
	Checksum       string
	PolicyDecision IngestionStatus
	CreatedAt      time.Time
}

func (j IngestionJob) Validate() error {
	if err := j.TenantID.Validate(); err != nil {
		return err
	}
	if j.JobID == "" {
		return errors.New("job_id is required")
	}
	if j.SourceURI == "" {
		return errors.New("source_uri is required")
	}
	if j.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	return nil
}

type RetrievalQuery struct {
	TenantID  TenantID
	RequestID string
	Text      string
	TopK      int
}

func (q RetrievalQuery) Validate() error {
	if err := q.TenantID.Validate(); err != nil {
		return err
	}
	if q.RequestID == "" {
		return errors.New("request_id is required")
	}
	if q.Text == "" {
		return errors.New("query text is required")
	}
	if q.TopK <= 0 {
		return errors.New("top_k must be > 0")
	}
	return nil
}

type RetrievalDocument struct {
	DocumentID string
	TenantID   TenantID
	Content    string
	Score      float64
}

type RetrievalResult struct {
	TenantID  TenantID
	RequestID string
	Documents []RetrievalDocument
}

func (r RetrievalResult) Validate() error {
	if err := r.TenantID.Validate(); err != nil {
		return err
	}
	if r.RequestID == "" {
		return errors.New("request_id is required")
	}
	return nil
}

type RerankCandidate struct {
	DocumentID string
	Text       string
	Score      float64
}

type RerankRequest struct {
	TenantID   TenantID
	RequestID  string
	QueryText  string
	Candidates []RerankCandidate
	TopN       int
}

func (r RerankRequest) Validate() error {
	if err := r.TenantID.Validate(); err != nil {
		return err
	}
	if r.RequestID == "" {
		return errors.New("request_id is required")
	}
	if r.QueryText == "" {
		return errors.New("query_text is required")
	}
	if len(r.Candidates) == 0 {
		return errors.New("at least one candidate is required")
	}
	if r.TopN <= 0 {
		return errors.New("top_n must be > 0")
	}
	return nil
}

type AuditOutcome string

const (
	AuditOutcomeAllowed AuditOutcome = "allowed"
	AuditOutcomeDenied  AuditOutcome = "denied"
	AuditOutcomeError   AuditOutcome = "error"
)

type AuditEvent struct {
	EventID    string
	TenantID   TenantID
	EventType  string
	Resource   string
	Actor      string
	Outcome    AuditOutcome
	OccurredAt time.Time
	Attributes map[string]string
}

func (a AuditEvent) Validate() error {
	if err := a.TenantID.Validate(); err != nil {
		return err
	}
	if a.EventID == "" {
		return errors.New("event_id is required")
	}
	if a.EventType == "" {
		return errors.New("event_type is required")
	}
	if a.OccurredAt.IsZero() {
		return errors.New("occurred_at is required")
	}
	return nil
}

// Authorizer is an extension point for tenant-aware auth checks.
type Authorizer interface {
	Authorize(ctx context.Context, claims AuthClaims, metadata RequestMetadata) error
}

// RateLimiter is an extension point for per-tenant quota and burst enforcement.
type RateLimiter interface {
	Allow(ctx context.Context, tenantID TenantID, resource string) (RateLimitDecision, error)
}

type RateLimitDecision struct {
	Allowed    bool
	Reason     string
	Remaining  int
	RetryAfter time.Duration
}

func (d RateLimitDecision) Validate() error {
	if d.Allowed {
		return nil
	}
	if d.Reason == "" {
		return errors.New("reason is required when request is denied")
	}
	return nil
}

type AuditSink interface {
	Write(ctx context.Context, event AuditEvent) error
}

func WrapValidationErr(contract string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s contract validation failed: %w", contract, err)
}
