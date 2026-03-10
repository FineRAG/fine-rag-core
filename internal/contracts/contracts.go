package contracts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
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

type QueueMessage struct {
	MessageID      string
	Job            IngestionJob
	IdempotencyKey string
	Attempt        int
	EnqueuedAt     time.Time
}

func (m QueueMessage) Validate() error {
	if m.MessageID == "" {
		return errors.New("message_id is required")
	}
	if err := m.Job.Validate(); err != nil {
		return fmt.Errorf("job invalid: %w", err)
	}
	if m.Attempt < 0 {
		return errors.New("attempt must be >= 0")
	}
	if m.EnqueuedAt.IsZero() {
		return errors.New("enqueued_at is required")
	}
	return nil
}

type IngestionQueueProducer interface {
	Enqueue(ctx context.Context, message QueueMessage) error
}

type IngestionQueueConsumer interface {
	Dequeue(ctx context.Context) (QueueMessage, error)
}

type DeadLetterQueue interface {
	Publish(ctx context.Context, message QueueMessage, reason string) error
}

type ArtifactBlobStore interface {
	Get(ctx context.Context, tenantID TenantID, sourceURI string) ([]byte, error)
}

type EmbeddingProvider interface {
	Embed(ctx context.Context, tenantID TenantID, chunks []string) ([][]float32, error)
}

type VectorRecord struct {
	RecordID   string
	TenantID   TenantID
	JobID      string
	ChunkText  string
	Embedding  []float32
	Metadata   map[string]string
	IndexedAt  time.Time
	SourceURI  string
	Checksum   string
	RetryCount int
}

func (r VectorRecord) Validate() error {
	if r.RecordID == "" {
		return errors.New("record_id is required")
	}
	if err := r.TenantID.Validate(); err != nil {
		return err
	}
	if r.JobID == "" {
		return errors.New("job_id is required")
	}
	if strings.TrimSpace(r.ChunkText) == "" {
		return errors.New("chunk_text is required")
	}
	if len(r.Embedding) == 0 {
		return errors.New("embedding is required")
	}
	if r.IndexedAt.IsZero() {
		return errors.New("indexed_at is required")
	}
	if r.SourceURI == "" {
		return errors.New("source_uri is required")
	}
	if r.Checksum == "" {
		return errors.New("checksum is required")
	}
	if r.RetryCount < 0 {
		return errors.New("retry_count must be >= 0")
	}
	return nil
}

type VectorIndex interface {
	Upsert(ctx context.Context, records []VectorRecord) error
}

type IdempotencyStore interface {
	Exists(ctx context.Context, key string) (bool, error)
	Save(ctx context.Context, key string, ttl time.Duration) error
}

type RetentionClass string

const (
	RetentionClassRawBlob    RetentionClass = "raw_blob"
	RetentionClassChunk      RetentionClass = "chunk"
	RetentionClassAuditEvent RetentionClass = "audit_event"
)

type RetentionRecord struct {
	TenantID       TenantID
	JobID          string
	Class          RetentionClass
	Resource       string
	RetentionUntil time.Time
}

func (r RetentionRecord) Validate() error {
	if err := r.TenantID.Validate(); err != nil {
		return err
	}
	if r.JobID == "" {
		return errors.New("job_id is required")
	}
	if r.Class == "" {
		return errors.New("retention class is required")
	}
	if strings.TrimSpace(r.Resource) == "" {
		return errors.New("resource is required")
	}
	if r.RetentionUntil.IsZero() {
		return errors.New("retention_until is required")
	}
	return nil
}

type RetentionPolicyHook interface {
	Apply(ctx context.Context, record RetentionRecord) error
}

type PayloadClassification string

const (
	PayloadClassValid          PayloadClassification = "valid"
	PayloadClassInvalidPayload PayloadClassification = "invalid_payload"
	PayloadClassInvalidTenant  PayloadClassification = "invalid_tenant"
	PayloadClassInvalidSource  PayloadClassification = "invalid_source"
)

type IngestionMetadata struct {
	TenantID       TenantID
	ChecksumSHA256 string
	SourceURI      string
	LifecycleClass string
	CapturedAt     time.Time
}

type TenantRecord struct {
	TenantID    TenantID
	DisplayName string
	PlanTier    string
	Active      bool
	UpdatedAt   time.Time
}

func (t TenantRecord) Validate() error {
	if err := t.TenantID.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(t.DisplayName) == "" {
		return errors.New("display_name is required")
	}
	if strings.TrimSpace(t.PlanTier) == "" {
		return errors.New("plan_tier is required")
	}
	if t.UpdatedAt.IsZero() {
		return errors.New("updated_at is required")
	}
	return nil
}

func (m IngestionMetadata) Validate() error {
	if err := m.TenantID.Validate(); err != nil {
		return err
	}
	if m.ChecksumSHA256 == "" {
		return errors.New("checksum_sha256 is required")
	}
	if m.SourceURI == "" {
		return errors.New("source_uri is required")
	}
	if m.LifecycleClass == "" {
		return errors.New("lifecycle_class is required")
	}
	if m.CapturedAt.IsZero() {
		return errors.New("captured_at is required")
	}
	return nil
}

type IngestionProfile struct {
	Metadata       IngestionMetadata
	PayloadBytes   int
	ContentType    string
	LineCount      int
	WordCount      int
	Classification PayloadClassification
	ErrorReason    string
}

func (p IngestionProfile) Validate() error {
	if err := p.Metadata.Validate(); err != nil {
		return err
	}
	if p.PayloadBytes < 0 {
		return errors.New("payload_bytes must be >= 0")
	}
	if p.Classification == "" {
		return errors.New("classification is required")
	}
	if p.Classification != PayloadClassValid && p.ErrorReason == "" {
		return errors.New("error_reason is required for invalid classifications")
	}
	return nil
}

type PolicyEngineInput struct {
	TenantID       TenantID
	ChecksumSHA256 string
	SourceURI      string
	LifecycleClass string
	Classification PayloadClassification
	ErrorReason    string
}

func (p IngestionProfile) ToPolicyEngineInput() PolicyEngineInput {
	return PolicyEngineInput{
		TenantID:       p.Metadata.TenantID,
		ChecksumSHA256: p.Metadata.ChecksumSHA256,
		SourceURI:      p.Metadata.SourceURI,
		LifecycleClass: p.Metadata.LifecycleClass,
		Classification: p.Classification,
		ErrorReason:    p.ErrorReason,
	}
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
	SourceURI  string
	Metadata   map[string]string
}

type RetrievalTrace struct {
	TenantFilterApplied  bool
	CandidateCount       int
	RerankApplied        bool
	FallbackReason       string
	DurationMillis       int64
	VectorProvider       string
	VectorStatus         string
	VectorLatencyMillis  int64
	GatewayProvider      string
	GatewayStatus        string
	GatewayModel         string
	GatewayLatencyMillis int64
	GatewayTokenInput    int
	GatewayTokenOutput   int
	GatewayTokenTotal    int
}

type RetrievalResult struct {
	TenantID  TenantID
	RequestID string
	Documents []RetrievalDocument
	Citations []string
	Trace     RetrievalTrace
}

func (r RetrievalResult) Validate() error {
	if err := r.TenantID.Validate(); err != nil {
		return err
	}
	if r.RequestID == "" {
		return errors.New("request_id is required")
	}
	for _, doc := range r.Documents {
		if doc.DocumentID == "" {
			return errors.New("document_id is required")
		}
		if err := doc.TenantID.Validate(); err != nil {
			return err
		}
		if doc.TenantID != r.TenantID {
			return errors.New("document tenant mismatch")
		}
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

func (r RerankRequest) TopCandidates() []RerankCandidate {
	copyCandidates := make([]RerankCandidate, len(r.Candidates))
	copy(copyCandidates, r.Candidates)
	sort.SliceStable(copyCandidates, func(i, j int) bool {
		return copyCandidates[i].Score > copyCandidates[j].Score
	})
	if r.TopN <= 0 || r.TopN >= len(copyCandidates) {
		return copyCandidates
	}
	return copyCandidates[:r.TopN]
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

type TenantRegistryRepository interface {
	Upsert(ctx context.Context, tenant TenantRecord) error
	GetByTenantID(ctx context.Context, tenantID TenantID) (TenantRecord, error)
}

type IngestionMetadataRepository interface {
	Save(ctx context.Context, metadata IngestionMetadata) error
	GetByChecksum(ctx context.Context, tenantID TenantID, checksumSHA256 string) (IngestionMetadata, error)
}

type AuditEventRepository interface {
	Save(ctx context.Context, event AuditEvent) error
	ListByTenant(ctx context.Context, tenantID TenantID, limit int) ([]AuditEvent, error)
}

type VectorSearcher interface {
	Search(ctx context.Context, tenantID TenantID, queryText string, topK int) ([]RetrievalDocument, error)
}

type VectorEmbeddingSearcher interface {
	SearchByEmbedding(ctx context.Context, tenantID TenantID, queryEmbedding []float32, topK int) ([]RetrievalDocument, error)
}

type QueryRewriter interface {
	RewriteQuery(ctx context.Context, tenantID TenantID, originalQuery string) (string, error)
}

type Reranker interface {
	Rerank(ctx context.Context, req RerankRequest) ([]RerankCandidate, error)
}

type ProviderErrorCategory string

const (
	ProviderErrValidation   ProviderErrorCategory = "validation"
	ProviderErrUnavailable  ProviderErrorCategory = "unavailable"
	ProviderErrTimeout      ProviderErrorCategory = "timeout"
	ProviderErrUnauthorized ProviderErrorCategory = "unauthorized"
	ProviderErrInternal     ProviderErrorCategory = "internal"
)

type ProviderError struct {
	Category ProviderErrorCategory
	Provider string
	Op       string
	Err      error
}

func (e ProviderError) Error() string {
	return fmt.Sprintf("%s provider error (%s): %s", e.Provider, e.Category, e.Op)
}

func (e ProviderError) Unwrap() error { return e.Err }

type VectorCallTrace struct {
	Provider      string
	Status        string
	LatencyMillis int64
}

type GatewayCallTrace struct {
	Provider       string
	Model          string
	LatencyMillis  int64
	TokenInput     int
	TokenOutput    int
	TokenTotal     int
	Status         string
	FallbackReason string
}

type VectorTraceProvider interface {
	LastVectorTrace() VectorCallTrace
}

type GatewayTraceProvider interface {
	LastGatewayTrace() GatewayCallTrace
}

func WrapValidationErr(contract string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s contract validation failed: %w", contract, err)
}
