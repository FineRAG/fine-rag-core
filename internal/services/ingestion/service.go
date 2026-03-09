package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"enterprise-go-rag/internal/contracts"
)

// Service defines ingestion orchestration boundaries for E1 foundations.
type Service interface {
	CreateJob(ctx context.Context, metadata contracts.RequestMetadata, job contracts.IngestionJob) (contracts.IngestionJob, error)
	GetJob(ctx context.Context, metadata contracts.RequestMetadata, jobID string) (contracts.IngestionJob, error)
}

type ProfileInput struct {
	TenantID       contracts.TenantID
	SourceURI      string
	LifecycleClass string
	ContentType    string
	Payload        []byte
}

type Profiler interface {
	Profile(ctx context.Context, in ProfileInput) (contracts.IngestionProfile, error)
}

type DeterministicProfiler struct{}

var ErrQueueEmpty = errors.New("ingestion queue empty")

func (DeterministicProfiler) Profile(_ context.Context, in ProfileInput) (contracts.IngestionProfile, error) {
	if err := in.TenantID.Validate(); err != nil {
		return invalidProfile(in, contracts.PayloadClassInvalidTenant, "tenant_id_missing"), nil
	}
	if strings.TrimSpace(in.SourceURI) == "" {
		return invalidProfile(in, contracts.PayloadClassInvalidSource, "source_uri_missing"), nil
	}
	if len(in.Payload) == 0 {
		return invalidProfile(in, contracts.PayloadClassInvalidPayload, "payload_empty"), nil
	}

	sum := sha256.Sum256(in.Payload)
	payloadText := string(in.Payload)

	profile := contracts.IngestionProfile{
		Metadata: contracts.IngestionMetadata{
			TenantID:       in.TenantID,
			ChecksumSHA256: hex.EncodeToString(sum[:]),
			SourceURI:      in.SourceURI,
			LifecycleClass: fallbackLifecycleClass(in.LifecycleClass),
			CapturedAt:     time.Now().UTC().Round(0),
		},
		PayloadBytes:   len(in.Payload),
		ContentType:    fallbackContentType(in.ContentType),
		LineCount:      countLines(payloadText),
		WordCount:      len(strings.Fields(payloadText)),
		Classification: contracts.PayloadClassValid,
	}

	if err := profile.Validate(); err != nil {
		return contracts.IngestionProfile{}, fmt.Errorf("profile validation failed: %w", err)
	}

	return profile, nil
}

func invalidProfile(in ProfileInput, class contracts.PayloadClassification, reason string) contracts.IngestionProfile {
	checksum := ""
	if len(in.Payload) > 0 {
		sum := sha256.Sum256(in.Payload)
		checksum = hex.EncodeToString(sum[:])
	}

	return contracts.IngestionProfile{
		Metadata: contracts.IngestionMetadata{
			TenantID:       in.TenantID,
			ChecksumSHA256: checksum,
			SourceURI:      in.SourceURI,
			LifecycleClass: fallbackLifecycleClass(in.LifecycleClass),
			CapturedAt:     time.Now().UTC().Round(0),
		},
		PayloadBytes:   len(in.Payload),
		ContentType:    fallbackContentType(in.ContentType),
		LineCount:      countLines(string(in.Payload)),
		WordCount:      len(strings.Fields(string(in.Payload))),
		Classification: class,
		ErrorReason:    reason,
	}
}

func fallbackLifecycleClass(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "standard"
	}
	return v
}

func fallbackContentType(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "application/octet-stream"
	}
	return v
}

func countLines(v string) int {
	if v == "" {
		return 0
	}
	return strings.Count(v, "\n") + 1
}

type WorkerConfig struct {
	MaxRetries     int
	ChunkSize      int
	IdempotencyTTL time.Duration
	Clock          func() time.Time
}

type DeterministicAsyncWorker struct {
	producer  contracts.IngestionQueueProducer
	consumer  contracts.IngestionQueueConsumer
	dlq       contracts.DeadLetterQueue
	blobStore contracts.ArtifactBlobStore
	embedder  contracts.EmbeddingProvider
	index     contracts.VectorIndex
	idem      contracts.IdempotencyStore
	retention contracts.RetentionPolicyHook
	clock     func() time.Time

	maxRetries     int
	chunkSize      int
	idempotencyTTL time.Duration
}

func NewDeterministicAsyncWorker(
	producer contracts.IngestionQueueProducer,
	consumer contracts.IngestionQueueConsumer,
	dlq contracts.DeadLetterQueue,
	blobStore contracts.ArtifactBlobStore,
	embedder contracts.EmbeddingProvider,
	index contracts.VectorIndex,
	idem contracts.IdempotencyStore,
	retention contracts.RetentionPolicyHook,
	config WorkerConfig,
) *DeterministicAsyncWorker {
	clock := config.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC().Round(0) }
	}
	maxRetries := config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	chunkSize := config.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 200
	}
	idemTTL := config.IdempotencyTTL
	if idemTTL <= 0 {
		idemTTL = 24 * time.Hour
	}

	return &DeterministicAsyncWorker{
		producer:       producer,
		consumer:       consumer,
		dlq:            dlq,
		blobStore:      blobStore,
		embedder:       embedder,
		index:          index,
		idem:           idem,
		retention:      retention,
		clock:          clock,
		maxRetries:     maxRetries,
		chunkSize:      chunkSize,
		idempotencyTTL: idemTTL,
	}
}

func (w *DeterministicAsyncWorker) EnqueueApprovedJob(ctx context.Context, metadata contracts.RequestMetadata, job contracts.IngestionJob) (contracts.QueueMessage, error) {
	if err := metadata.Validate(); err != nil {
		return contracts.QueueMessage{}, contracts.WrapValidationErr("request_metadata", err)
	}
	if err := job.Validate(); err != nil {
		return contracts.QueueMessage{}, contracts.WrapValidationErr("ingestion_job", err)
	}
	if metadata.TenantID != job.TenantID {
		return contracts.QueueMessage{}, fmt.Errorf("tenant mismatch: metadata=%s job=%s", metadata.TenantID, job.TenantID)
	}
	if job.PolicyDecision != contracts.IngestionStatusApproved {
		return contracts.QueueMessage{}, fmt.Errorf("job policy decision must be approved, got %s", job.PolicyDecision)
	}

	message := contracts.QueueMessage{
		MessageID:      hashParts("msg", string(job.TenantID), job.JobID, job.Checksum),
		Job:            job,
		IdempotencyKey: idempotencyKey(job),
		Attempt:        0,
		EnqueuedAt:     w.clock(),
	}
	if err := message.Validate(); err != nil {
		return contracts.QueueMessage{}, contracts.WrapValidationErr("queue_message", err)
	}
	if w.producer == nil {
		return contracts.QueueMessage{}, errors.New("queue producer is required")
	}
	if err := w.producer.Enqueue(ctx, message); err != nil {
		return contracts.QueueMessage{}, err
	}
	return message, nil
}

func (w *DeterministicAsyncWorker) ProcessNext(ctx context.Context) error {
	if w.consumer == nil {
		return errors.New("queue consumer is required")
	}
	message, err := w.consumer.Dequeue(ctx)
	if err != nil {
		return err
	}
	return w.ProcessMessage(ctx, message)
}

func (w *DeterministicAsyncWorker) ProcessMessage(ctx context.Context, message contracts.QueueMessage) error {
	if err := message.Validate(); err != nil {
		return contracts.WrapValidationErr("queue_message", err)
	}
	if message.IdempotencyKey == "" {
		message.IdempotencyKey = idempotencyKey(message.Job)
	}

	if w.idem != nil {
		exists, err := w.idem.Exists(ctx, message.IdempotencyKey)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	}

	payload, err := w.loadPayload(ctx, message.Job)
	if err != nil {
		return w.retryOrDLQ(ctx, message, "blob_fetch_failed", err)
	}

	chunks := chunkPayload(payload, w.chunkSize)
	if len(chunks) == 0 {
		return w.retryOrDLQ(ctx, message, "chunking_failed", errors.New("no chunks generated"))
	}

	if w.embedder == nil {
		return errors.New("embedding provider is required")
	}
	vectors, err := w.embedder.Embed(ctx, message.Job.TenantID, chunks)
	if err != nil {
		return w.retryOrDLQ(ctx, message, "embedding_failed", err)
	}
	if len(vectors) != len(chunks) {
		return w.retryOrDLQ(ctx, message, "embedding_count_mismatch", fmt.Errorf("got=%d want=%d", len(vectors), len(chunks)))
	}

	records := make([]contracts.VectorRecord, 0, len(chunks))
	now := w.clock()
	for i := range chunks {
		record := contracts.VectorRecord{
			RecordID:  hashParts("vec", string(message.Job.TenantID), message.Job.JobID, message.Job.Checksum, fmt.Sprintf("%d", i)),
			TenantID:  message.Job.TenantID,
			JobID:     message.Job.JobID,
			ChunkText: chunks[i],
			Embedding: vectors[i],
			Metadata: map[string]string{
				"tenant_id":       string(message.Job.TenantID),
				"job_id":          message.Job.JobID,
				"source_uri":      message.Job.SourceURI,
				"idempotency_key": message.IdempotencyKey,
			},
			IndexedAt:  now,
			SourceURI:  message.Job.SourceURI,
			Checksum:   message.Job.Checksum,
			RetryCount: message.Attempt,
		}
		if err := record.Validate(); err != nil {
			return contracts.WrapValidationErr("vector_record", err)
		}
		records = append(records, record)
	}

	if w.index == nil {
		return errors.New("vector index is required")
	}
	if err := w.index.Upsert(ctx, records); err != nil {
		return w.retryOrDLQ(ctx, message, "vector_upsert_failed", err)
	}

	if w.idem != nil {
		if err := w.idem.Save(ctx, message.IdempotencyKey, w.idempotencyTTL); err != nil {
			return err
		}
	}

	if w.retention != nil {
		retentionSet := []contracts.RetentionRecord{
			{
				TenantID:       message.Job.TenantID,
				JobID:          message.Job.JobID,
				Class:          contracts.RetentionClassRawBlob,
				Resource:       message.Job.SourceURI,
				RetentionUntil: now.Add(30 * 24 * time.Hour),
			},
			{
				TenantID:       message.Job.TenantID,
				JobID:          message.Job.JobID,
				Class:          contracts.RetentionClassChunk,
				Resource:       message.Job.JobID,
				RetentionUntil: now.Add(90 * 24 * time.Hour),
			},
			{
				TenantID:       message.Job.TenantID,
				JobID:          message.Job.JobID,
				Class:          contracts.RetentionClassAuditEvent,
				Resource:       "governance.ingestion.policy_decision",
				RetentionUntil: now.Add(365 * 24 * time.Hour),
			},
		}
		for _, record := range retentionSet {
			if err := record.Validate(); err != nil {
				return contracts.WrapValidationErr("retention_record", err)
			}
			if err := w.retention.Apply(ctx, record); err != nil {
				return err
			}
		}
	}

	return nil
}

func (w *DeterministicAsyncWorker) ReplayDLQMessage(ctx context.Context, message contracts.QueueMessage) error {
	if w.producer == nil {
		return errors.New("queue producer is required")
	}
	message.Attempt = 0
	message.EnqueuedAt = w.clock()
	if message.IdempotencyKey == "" {
		message.IdempotencyKey = idempotencyKey(message.Job)
	}
	if err := message.Validate(); err != nil {
		return contracts.WrapValidationErr("queue_message", err)
	}
	return w.producer.Enqueue(ctx, message)
}

func (w *DeterministicAsyncWorker) loadPayload(ctx context.Context, job contracts.IngestionJob) ([]byte, error) {
	if w.blobStore == nil {
		return nil, errors.New("artifact blob store is required")
	}
	return w.blobStore.Get(ctx, job.TenantID, job.SourceURI)
}

func (w *DeterministicAsyncWorker) retryOrDLQ(ctx context.Context, message contracts.QueueMessage, reason string, cause error) error {
	nextAttempt := message.Attempt + 1
	if nextAttempt <= w.maxRetries && w.producer != nil {
		message.Attempt = nextAttempt
		message.EnqueuedAt = w.clock()
		if err := w.producer.Enqueue(ctx, message); err != nil {
			return fmt.Errorf("%s: %w", reason, err)
		}
		return fmt.Errorf("%s: %w", reason, cause)
	}
	if w.dlq != nil {
		if err := w.dlq.Publish(ctx, message, reason); err != nil {
			return fmt.Errorf("%s: %w", reason, err)
		}
	}
	return fmt.Errorf("%s: %w", reason, cause)
}

func idempotencyKey(job contracts.IngestionJob) string {
	return hashParts("idem", string(job.TenantID), job.JobID, job.Checksum)
}

func hashParts(parts ...string) string {
	joined := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:])
}

func chunkPayload(payload []byte, maxChunkSize int) []string {
	text := strings.TrimSpace(string(payload))
	if text == "" {
		return nil
	}
	if maxChunkSize <= 0 {
		maxChunkSize = 200
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	chunks := make([]string, 0, len(words)/10+1)
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if len(candidate) > maxChunkSize && current != "" {
			chunks = append(chunks, current)
			current = word
			continue
		}
		current = candidate
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}
