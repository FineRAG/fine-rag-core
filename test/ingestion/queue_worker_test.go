package ingestion_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/services/ingestion"
)

type memoryQueue struct {
	messages []contracts.QueueMessage
}

func (q *memoryQueue) Enqueue(_ context.Context, message contracts.QueueMessage) error {
	q.messages = append(q.messages, message)
	return nil
}

func (q *memoryQueue) Dequeue(_ context.Context) (contracts.QueueMessage, error) {
	if len(q.messages) == 0 {
		return contracts.QueueMessage{}, ingestion.ErrQueueEmpty
	}
	message := q.messages[0]
	q.messages = q.messages[1:]
	return message, nil
}

type memoryBlobStore struct {
	payloadBySource map[string][]byte
	reads           []string
}

func (m *memoryBlobStore) Get(_ context.Context, _ contracts.TenantID, sourceURI string) ([]byte, error) {
	m.reads = append(m.reads, sourceURI)
	payload, ok := m.payloadBySource[sourceURI]
	if !ok {
		return nil, errors.New("blob not found")
	}
	return payload, nil
}

type memoryEmbedder struct{}

func (memoryEmbedder) Embed(_ context.Context, _ contracts.TenantID, chunks []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(chunks))
	for i, chunk := range chunks {
		vectors = append(vectors, []float32{float32(len(chunk)), float32(i + 1)})
	}
	return vectors, nil
}

type flakyEmbedder struct {
	failuresRemaining int
}

func (f *flakyEmbedder) Embed(_ context.Context, _ contracts.TenantID, chunks []string) ([][]float32, error) {
	if f.failuresRemaining > 0 {
		f.failuresRemaining--
		return nil, errors.New("embedder unavailable")
	}
	vectors := make([][]float32, 0, len(chunks))
	for i := range chunks {
		vectors = append(vectors, []float32{1, float32(i + 1)})
	}
	return vectors, nil
}

type memoryVectorIndex struct {
	upserts [][]contracts.VectorRecord
}

func (m *memoryVectorIndex) Upsert(_ context.Context, records []contracts.VectorRecord) error {
	copied := make([]contracts.VectorRecord, len(records))
	copy(copied, records)
	m.upserts = append(m.upserts, copied)
	return nil
}

type memoryIdempotencyStore struct {
	keys map[string]time.Duration
}

func (m *memoryIdempotencyStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.keys[key]
	return ok, nil
}

func (m *memoryIdempotencyStore) Save(_ context.Context, key string, ttl time.Duration) error {
	if m.keys == nil {
		m.keys = map[string]time.Duration{}
	}
	m.keys[key] = ttl
	return nil
}

type memoryDLQ struct {
	entries []string
}

func (m *memoryDLQ) Publish(_ context.Context, message contracts.QueueMessage, reason string) error {
	m.entries = append(m.entries, fmt.Sprintf("%s:%s", message.MessageID, reason))
	return nil
}

type memoryRetentionHook struct {
	records []contracts.RetentionRecord
}

func (m *memoryRetentionHook) Apply(_ context.Context, record contracts.RetentionRecord) error {
	m.records = append(m.records, record)
	return nil
}

func fixedWorkerNow() time.Time {
	return time.Date(2026, 3, 9, 11, 12, 13, 0, time.UTC)
}

func approvedJob() contracts.IngestionJob {
	return contracts.IngestionJob{
		JobID:          "job-e2t3-1",
		TenantID:       "tenant-a",
		SourceURI:      "s3://tenant-a-ap-south-1/docs/manual.txt",
		Checksum:       "sum-1",
		PolicyDecision: contracts.IngestionStatusApproved,
		CreatedAt:      fixedWorkerNow(),
	}
}

func requestMeta() contracts.RequestMetadata {
	return contracts.RequestMetadata{TenantID: "tenant-a", RequestID: "req-e2t3-1"}
}

func newWorkerForTests(
	queue *memoryQueue,
	blob *memoryBlobStore,
	embed contracts.EmbeddingProvider,
	index *memoryVectorIndex,
	idem *memoryIdempotencyStore,
	dlq *memoryDLQ,
	retention *memoryRetentionHook,
	maxRetries int,
) *ingestion.DeterministicAsyncWorker {
	return ingestion.NewDeterministicAsyncWorker(
		queue,
		queue,
		dlq,
		blob,
		embed,
		index,
		idem,
		retention,
		ingestion.WorkerConfig{
			MaxRetries:     maxRetries,
			ChunkSize:      28,
			IdempotencyTTL: 12 * time.Hour,
			Clock:          fixedWorkerNow,
		},
	)
}

func TestQueueWorkerIndexesApprovedArtifactsWithTenantScope(t *testing.T) {
	queue := &memoryQueue{}
	blob := &memoryBlobStore{payloadBySource: map[string][]byte{
		"s3://tenant-a-ap-south-1/docs/manual.txt": []byte("alpha beta gamma delta epsilon zeta eta theta"),
	}}
	index := &memoryVectorIndex{}
	idem := &memoryIdempotencyStore{keys: map[string]time.Duration{}}
	dlq := &memoryDLQ{}
	retention := &memoryRetentionHook{}
	worker := newWorkerForTests(queue, blob, memoryEmbedder{}, index, idem, dlq, retention, 2)

	message, err := worker.EnqueueApprovedJob(t.Context(), requestMeta(), approvedJob())
	if err != nil {
		t.Fatalf("enqueue approved: %v", err)
	}
	if message.IdempotencyKey == "" {
		t.Fatal("expected idempotency key")
	}

	if err := worker.ProcessNext(t.Context()); err != nil {
		t.Fatalf("process next: %v", err)
	}

	if len(index.upserts) != 1 {
		t.Fatalf("expected single index upsert, got %d", len(index.upserts))
	}
	if len(index.upserts[0]) == 0 {
		t.Fatal("expected at least one vector record")
	}
	for _, record := range index.upserts[0] {
		if record.TenantID != "tenant-a" {
			t.Fatalf("tenant scope mismatch in indexed record: %+v", record)
		}
		if record.Metadata["tenant_id"] != "tenant-a" || record.Metadata["job_id"] != "job-e2t3-1" {
			t.Fatalf("missing tenant metadata on indexed record: %+v", record.Metadata)
		}
	}
	if len(idem.keys) != 1 {
		t.Fatalf("expected idempotency key saved once, got %d", len(idem.keys))
	}
	if len(retention.records) != 3 {
		t.Fatalf("expected retention hooks for 3 classes, got %d", len(retention.records))
	}
	if len(dlq.entries) != 0 {
		t.Fatalf("expected no DLQ messages, got %d", len(dlq.entries))
	}
}

func TestIdempotencySkipsDuplicateQueueWorkerSubmission(t *testing.T) {
	queue := &memoryQueue{}
	blob := &memoryBlobStore{payloadBySource: map[string][]byte{
		"s3://tenant-a-ap-south-1/docs/manual.txt": []byte("same payload for duplicate ingestion"),
	}}
	index := &memoryVectorIndex{}
	idem := &memoryIdempotencyStore{keys: map[string]time.Duration{}}
	dlq := &memoryDLQ{}
	retention := &memoryRetentionHook{}
	worker := newWorkerForTests(queue, blob, memoryEmbedder{}, index, idem, dlq, retention, 2)

	message, err := worker.EnqueueApprovedJob(t.Context(), requestMeta(), approvedJob())
	if err != nil {
		t.Fatalf("enqueue approved: %v", err)
	}

	if err := worker.ProcessMessage(t.Context(), message); err != nil {
		t.Fatalf("first process: %v", err)
	}
	if err := worker.ProcessMessage(t.Context(), message); err != nil {
		t.Fatalf("second process: %v", err)
	}

	if len(index.upserts) != 1 {
		t.Fatalf("expected exactly one upsert due to idempotency, got %d", len(index.upserts))
	}
}

func TestQueueWorkerRoutesFailuresToDLQAfterRetryExhaustion(t *testing.T) {
	queue := &memoryQueue{}
	blob := &memoryBlobStore{payloadBySource: map[string][]byte{
		"s3://tenant-a-ap-south-1/docs/manual.txt": []byte("alpha beta gamma"),
	}}
	index := &memoryVectorIndex{}
	idem := &memoryIdempotencyStore{keys: map[string]time.Duration{}}
	dlq := &memoryDLQ{}
	retention := &memoryRetentionHook{}
	embedder := &flakyEmbedder{failuresRemaining: 3}
	worker := newWorkerForTests(queue, blob, embedder, index, idem, dlq, retention, 1)

	_, err := worker.EnqueueApprovedJob(t.Context(), requestMeta(), approvedJob())
	if err != nil {
		t.Fatalf("enqueue approved: %v", err)
	}

	if err := worker.ProcessNext(t.Context()); err == nil {
		t.Fatal("expected retry-triggering error on first processing attempt")
	}
	if len(queue.messages) != 1 {
		t.Fatalf("expected one requeued message after retry, got %d", len(queue.messages))
	}
	if queue.messages[0].Attempt != 1 {
		t.Fatalf("expected retry attempt to increment to 1, got %d", queue.messages[0].Attempt)
	}

	if err := worker.ProcessNext(t.Context()); err == nil {
		t.Fatal("expected terminal error after retry exhaustion")
	}
	if len(dlq.entries) != 1 {
		t.Fatalf("expected one DLQ entry, got %d", len(dlq.entries))
	}
	if !strings.Contains(dlq.entries[0], "embedding_failed") {
		t.Fatalf("expected embedding_failed DLQ reason, got %q", dlq.entries[0])
	}
}

func TestMilvusS3IngestionE2EReplayFlow(t *testing.T) {
	queue := &memoryQueue{}
	blob := &memoryBlobStore{payloadBySource: map[string][]byte{
		"s3://tenant-a-ap-south-1/docs/manual.txt": []byte("first second third fourth fifth sixth"),
	}}
	index := &memoryVectorIndex{}
	idem := &memoryIdempotencyStore{keys: map[string]time.Duration{}}
	dlq := &memoryDLQ{}
	retention := &memoryRetentionHook{}
	worker := newWorkerForTests(queue, blob, memoryEmbedder{}, index, idem, dlq, retention, 2)

	message, err := worker.EnqueueApprovedJob(t.Context(), requestMeta(), approvedJob())
	if err != nil {
		t.Fatalf("enqueue approved: %v", err)
	}
	if err := worker.ProcessNext(t.Context()); err != nil {
		t.Fatalf("process initial message: %v", err)
	}

	if err := worker.ReplayDLQMessage(t.Context(), message); err != nil {
		t.Fatalf("replay message: %v", err)
	}
	if len(queue.messages) != 1 {
		t.Fatalf("expected replayed message in queue, got %d", len(queue.messages))
	}
	if queue.messages[0].Attempt != 0 {
		t.Fatalf("expected replay attempt reset to zero, got %d", queue.messages[0].Attempt)
	}

	if err := worker.ProcessNext(t.Context()); err != nil {
		t.Fatalf("process replayed message: %v", err)
	}

	if len(blob.reads) < 1 {
		t.Fatal("expected object store reads to occur")
	}
	if len(index.upserts) != 1 {
		t.Fatalf("expected Milvus/vector index upsert only once due to idempotency, got %d", len(index.upserts))
	}
}
