# Distilled Requirements - Go-RAG

Date: 2026-03-08
Status: Draft for Review (AnalyzerAgent)
Source: User clarifications + initial specification

## 1. Goals

* Build an enterprise-grade, multi-tenant Go (Golang) RAG platform.
* Enforce governed ingestion to avoid low-quality/unsafe knowledge entering retrieval.
* Optimize retrieval speed and accuracy under high traffic.
* Keep authoring and AI code generation on local machine; deploy/build on AWS EC2 via rsync pipeline.

## 2. Scope

### In scope (Milestone 1)

* Task 1: Core contracts and project skeleton.
* Task 2: Multi-tenant context and middleware.
* Task 3: Data profiler and quality gatekeeper.
* Task 4: Multi-modal blob handler.

### Deferred

* Task 5 Temporal workflow orchestration to v1.1.
* Full CI quality gate and broader production launch tasks after milestone completion.

## 3. Functional Requirements

* Multi-tenant logical isolation via mandatory tenant context and tenant filters.
* REST-first APIs; gRPC optional in later phase.
* Tenant registry persisted in PostgreSQL.
* Auth model: API key per tenant (no expiry, revoke-only lifecycle in v1).
* Lightweight tenant ingestion UI required:
  * Tenant login using tenant-id + password.
  * Upload dashboard for files and folders for ingestion.
* Lightweight tenant search UI required:
  * Search input for query execution.
  * Results view for retrieval responses.
* Vector store: Milvus.
* Blob store: MinIO (all environments for now).
* AI provider abstraction through gateway; default gateway Portkey.
* Embedding service: Qwen3-Embedding-4B as sidecar container.
* Retrieval quality includes reranking (cross-encoder) in v1 using a provider-hosted reranker API (managed).
* Async ingestion supported before Temporal (queue-based processing required).

## 4. Governance and Compliance

* PII redaction required.
* Data residency lock: AWS region ap-south-1.
* Metadata governance and approval workflow required (full Temporal + web UI flow targeted, but orchestration deferred to v1.1).
* Audit and cost records retention: 90 days.

## 5. Non-Functional Requirements

* Retrieval steady-state load target: 300 RPS.
* Explicit rate limiter required to enforce throughput limits with maximum 600 RPS burst cap.
* Default per-tenant quota: 2 RPS steady with 4 RPS burst.
* Retrieval latency target: p95 <\= 800 ms.
* Availability objective: 99.9%.
* Security baseline:
  * TLS in transit.
  * Encryption at rest for database and object storage.
* High availability and data cleanup are top priorities.

## 6. Observability and Metering

* OpenTelemetry instrumentation across critical paths.
* Metrics backend: Prometheus + Grafana.
* Cost attribution model by tenant must be supported in architecture (detailed implementation may span later milestones).

## 7. Deployment and Runtime

* Code generated locally in VS Code using AI agents.
* Sync local code to EC2 via rsync script.
* Build, containerization, and deployment executed on EC2.
* Runtime mode currently: Docker only.

## 8. Quality and Testing Requirements

* Unit tests for contracts and core middleware.
* Integration tests for tenant isolation and ingestion quality behavior.
* RAG evaluation gate approach selected: custom Go evaluator (instead of RAGas sidecar).

## 9. Risks and Assumptions

* Cross-encoder model/provider for reranking not yet finalized.
* Queue technology choice needs final lock (SQS vs Redis-based in Go ecosystem).
* Temporal deferral may require temporary orchestration glue that must be replaceable.

## 10. Finalized Clarifications

* API key lifecycle policy: no expiry, revoke-only in v1.
* Tenant quota defaults: 2 RPS steady per tenant with 4 RPS burst.
* Disaster recovery targets: RPO 24h, RTO 4h.
* Reranker hosting pattern: provider-hosted reranker API (managed).
* Data cleanup retention policy by class:
  * Raw blobs: 30 days.
  * Processed chunks: 90 days.
  * Audit logs: 180 days.