# E2-T3 Async Queue Worker and Indexing Path

## Objective

Implement queue-based ingestion processing, including chunking, embedding requests, tenant-scoped vector writes, retries, and DLQ behavior.

## Scope

1. Queue producer/consumer contracts for ingestion jobs (v1 queue mode).
2. Worker flow: fetch approved artifacts, chunk, embed, index with tenant filter.
3. Idempotency key enforcement across retries and duplicate submissions.
4. Retry/backoff, dead-letter routing, and replay operational controls.
5. Retention policy hooks for raw blob/chunk/audit lifecycle classes.

## Dependencies

1. Upstream required: `E2-T2`.
2. Downstream blocked by this task: `E3-T1` validation, end-to-end quality/perf gates.
3. Parallelization: can run in parallel with E3 dashboard prep after minimum indexed corpus is available.

## Acceptance Criteria

1. Approved artifacts are indexed with tenant-scoped Milvus metadata.
2. Duplicate ingestion requests remain idempotent under retries.
3. Worker failures route to DLQ after configured retry policy is exhausted.
4. Replay and recovery procedure is documented and testable.
5. Lifecycle retention job criteria are specified for each data class.

## Validation Commands

```bash
# Queue and worker pipeline behavior checks
go test ./... -run Queue|Worker|Idempotency -count=1

# Indexing and storage integration checks
go test ./... -run Milvus|MinIO|IngestionE2E -count=1
```

## Execution Tracking

- [ ] Started
- [ ] Completed
- Evidence:
- Notes:
