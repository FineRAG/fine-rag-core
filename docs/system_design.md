# System Design - Go-RAG

Date: 2026-03-08
Status: Revised for Review (ArchitectAgent)
Input: `docs/distilled_requirements.md`

## 1. Design Summary

Go-RAG v1 is a multi-tenant, REST-first Golang platform with governed ingestion and low-latency retrieval. The design enforces tenant isolation at middleware, storage, and query layers; runs in AWS `ap-south-1`; uses queue-based async ingestion (Temporal deferred to v1.1); and targets `300 RPS` steady retrieval, `600 RPS` burst cap, `p95 <= 800 ms`, and `99.9%` availability.

Backend language/runtime baseline for this project is `Go 1.26.1`.

## 2. Architecture Overview and Boundaries

### In Scope (v1)

* API service (REST), tenant context middleware, auth, rate limiting.
* Two frontend apps (React + Vite): ingestion control/dashboard and tenant search/query UI.
* Ingestion pipeline: profiler, policy checks, blob store, async queue workers.
* Retrieval pipeline: embed, Milvus search with tenant filter, managed reranker.
* Tenant registry + metadata in PostgreSQL.
* AWS S3 object storage.
* OTel + managed Prometheus + managed Grafana observability.

### Deferred (v1.1+)

* Temporal workflow orchestration and full HITL approval UI.
* gRPC transport.
* Broader production hardening outside milestone deliverables.

### Service Boundaries

* `ingestion-dashboard-ui` (React + Vite): tenant login, file/folder upload, ingestion status, API-key create/revoke UI.
* `search-query-ui` (React + Vite): query input, streaming answer panel, citation view, request status.
* `api-gateway`: request routing, authn, rate limits, tenant context, response shaping, and SSE streaming for generated responses.
* `ingestion-orchestrator`: ingestion state machine, policy gate evaluation, queue dispatch.
* `ingestion-worker`: chunking, embedding calls, vector writes, retry/DLQ handling.
* `retrieval-service`: query embedding, hybrid retrieval, rerank, citation assembly.
* `governance-service` (module in v1, separable service in v1.1): PII redaction, residency checks, metadata policy decisions.
* `storage adapters`: PostgreSQL (tenant/config/metadata/audit), S3 (blobs), Milvus (vectors).
* `ai-adapter`: Portkey gateway client + Qwen3-Embedding-4B sidecar routing.

## 3. Data Flow (Ingestion, Retrieval, Governance)

### Ingestion Flow (Async)

1. Tenant user uploads file/folder from lightweight UI to `POST /ingestion/jobs`.
2. API validates tenant credentials (tenant-id + password for UI session, API key for service calls) and applies per-tenant quota.
3. Governance gate runs profiler + PII checks and sets state: `approved`, `quarantine`, or `rejected`.
4. Raw blob stored in S3 path `tenant_id/class/date/object_key`.
5. Metadata record persisted in PostgreSQL (tenant, checksum, source, policy outcome, lifecycle TTL class).
6. Queue job emitted for `approved` artifacts.
7. Worker chunks content, requests embeddings from Qwen sidecar via AI adapter, writes vectors to Milvus with mandatory `tenant_id` scalar filter.
8. Audit and cost events emitted and retained by policy.

### Retrieval Flow

1. Tenant search UI calls `POST /search` with tenant context.
2. API enforces auth + rate limits + tenant context presence.
3. Retrieval service embeds query, executes Milvus search constrained by `tenant_id`.
4. Top-N candidates are reranked via managed provider-hosted cross-encoder API.
5. Retrieval service emits answer tokens via server-sent events (SSE), then finalizes with citations and trace metadata (`request_id`, retrieval timings, `ttft_ms`).

### Governance Controls in Flow

* PII redaction before indexing and before model/reranker calls.
* Region lock validation (`ap-south-1`) for all data-plane resources.
* Metadata approval state controls indexability.
* Data lifecycle jobs enforce class-based retention.

## 4. Tenant Isolation and Security Model

### Tenant Isolation

* Mandatory tenant context from edge to repository interfaces; requests without tenant fail fast (`400/401`).
* PostgreSQL row ownership enforced with tenant\_id predicates in repository layer.
* Milvus collections include tenant filter field and query-time mandatory filter.
* S3 object keys and bucket policies are tenant-scoped prefixes.
* Isolation integration tests required for ingest and retrieval leakage checks.

### Security Controls

* API key per tenant (no expiry in v1, revoke-only), stored hashed + salted.
* TLS in transit for client-to-API and service-to-service calls.
* Encryption at rest for PostgreSQL and S3 objects.
* Secrets from runtime secret manager (Vault or AWS Secrets Manager adapter).
* Audit log includes auth outcome, policy outcome, and high-risk admin actions.

## 5. Runtime and Deployment Topology

### Region and Runtime

* Single region: AWS `ap-south-1` (hard residency lock).
* Runtime: Docker containers on EC2.
* Build/deploy path: local authoring -> `rsync` to EC2 -> build and rollout on EC2.

### v1 Topology (Right-Sized)

* `EC2-AppPool` (2 instances minimum): API + retrieval service containers behind ALB.
* `EC2-Frontend` (1-2 instances or colocated containers): `ingestion-dashboard-ui` and `search-query-ui` served via Nginx.
* `EC2-WorkerPool` (1-2 instances): ingestion worker containers (scaled by queue depth).
* `PostgreSQL`: hosted platform instance (credentials supplied externally), daily snapshot policy validated by provider SLA.
* `Milvus`: hosted platform instance (credentials supplied externally), capacity tier sized for target corpus and QPS.
* `AWS S3`: bucket lifecycle policies enabled; IAM and bucket policy enforce least privilege.
* `Managed Prometheus/Grafana`: external observability services ingesting metrics and dashboards from the deployed stack.
* `SQS` (v1): managed queue for ingestion jobs with DLQ.

### v1.1+ Topology Expansion

* Add Temporal cluster and approval web app.
* Split governance module into independent service if policy complexity grows.

## 6. SLO and Scaling Strategy

### SLO Targets

* Availability: `99.9%` monthly.
* Retrieval latency: `p95 <= 800 ms` at `300 RPS` steady.
* Burst management: global cap `600 RPS`; default per-tenant `2 RPS` steady / `4 RPS` burst.

### Scaling Controls

* HPA-equivalent EC2 autoscaling by CPU + p95 latency + request queue depth.
* Worker autoscaling by ingestion queue depth and job age.
* Backpressure: reject or defer ingestion when worker lag exceeds threshold.
* Reranker bounded by top-K truncation and timeout fallback.

### Capacity Guardrails

* Reserve 20% headroom above forecast steady load.
* Enforce connection pool limits for PostgreSQL/Milvus.
* Load-test gates: 1-hour sustained 300 RPS + burst tests to 600 RPS.

## 7. Cost Model and Optimization Levers

### Primary Cost Drivers

* Embedding, generation, and reranker token usage (dominant variable cost).
* Milvus compute/storage.
* EC2 runtime footprint.
* Object storage growth and retention.
* Observability storage/cardinality.

### Optimization Recommendations (with Trade-offs)

1. Embedding cache keyed by `(tenant_id, content_hash, model_version)`.
   Trade-off: lower model spend and latency vs cache invalidation complexity.
2. Reranker route policy by traffic class.
   Trade-off: premium tenants use managed reranker; standard tier uses smaller top-K or optional bypass, reducing cost at possible relevance drop.
3. Queue platform lock for v1: AWS SQS.
   Trade-off: lower ops overhead and strong durability with DLQ vs per-request managed queue charges.
   Alternative for v1.1+: Redis streams if throughput and cost profile justifies added ops complexity.
4. Data lifecycle enforcement by class.
   Trade-off: raw blobs 30d, chunks 90d, audit 180d reduces storage spend vs limits long-horizon reprocessing.
5. Observability cardinality controls.
   Trade-off: hash or bucket high-cardinality labels to control Prometheus cost vs reduced per-request drill-down detail.
6. Right-sizing by phase.
   Trade-off: lean worker pool in v1 lowers spend; may increase ingest latency during spikes.

### Managed Reranker Provider Suggestions (v1)

1. Baseline vendor profile.
   Require p95 inference SLA <= 300 ms in-region equivalent path, 99.9% monthly availability, and clear token pricing.
2. Routing policy.
   Premium tenants: full rerank on top-50 candidates; standard tenants: top-20 rerank with budget cap.
3. Cost guardrail.
   Enforce per-tenant monthly rerank token budget with soft alerts at 70% and hard cap behavior at 100% (fallback to non-reranked ordering).

## 8. Failure Modes and Recovery Strategy

### Expected Failure Modes

* Queue backlog growth due to embedding/reranker slowdown.
* Milvus unavailability or degraded query latency.
* S3 object write/read failures.
* Tenant auth store (PostgreSQL) degradation.
* Upstream AI provider timeout/rate-limit.

### Recovery and Resilience

* Idempotent ingestion job keys + retry with exponential backoff + DLQ.
* Circuit breakers on AI provider and reranker calls; fallback to non-reranked retrieval on timeout budget breach.
* Readiness/liveness probes and automatic container restart.
* Snapshot and backup policy aligned to DR: `RPO 24h`, `RTO 4h`.
* Runbook-driven restore drills monthly; verify restore-to-service within RTO.

## 9. CI/CD and Quality Gates

### Build and Release Flow (EC2-Centric)

1. Local branch checks.
2. Sync to EC2 build host via `scripts/deploy_ec2.sh` (contract): `rsync` source, pull secrets, run `docker compose build`, run `docker compose up -d` for frontend, backend, and operational dependencies; observability is provided by managed Prometheus/Grafana outside the compose stack.
3. Container build, SBOM generation, image vulnerability scan.
4. Unit + integration tests.
5. Performance smoke and RAG evaluator gate.
6. Staged rollout and post-deploy smoke tests.

### Mandatory Gates

* Unit tests: contracts, tenant middleware, auth, rate limiter.
* Integration tests: tenant isolation, governance gate behavior, queue idempotency.
* RAG quality gate: custom Go evaluator threshold.
* Security gate: dependency and image scan thresholds.
* Rollback gate: automatic rollback on error budget burn or p95 regression.

### Observability and Metering Minimums

* Token metrics: embedding tokens, model input tokens, model output tokens, and reranker tokens by tenant.
* Latency metrics: TTFT, end-to-end latency, and stage timings (embed/search/rerank/generate).
* RED metrics: request rate, error rate, duration for API and retrieval pipelines.
* Budget alarms: per-tenant token-cost alarms and global anomaly detection.

## 10. Implementation Phases Aligned to Task Streams

### Phase 1 (Milestone v1: Tasks 1-4)

* Task 1: core contracts + module skeleton + service interfaces.
* Task 2: tenant context middleware + auth + quota/rate-limiter.
* Task 3: profiler + policy gate + metadata lifecycle state model.
* Task 4: multi-modal blob handler + async ingestion workers + Milvus index path.

### Phase 2 (v1 hardening)

* Load/perf tuning to meet 300 RPS and latency SLO.
* Observability dashboards for token usage, TTFT, and RED metrics + SLO alerts + DR drill automation.

### Phase 3 (v1.1)

* Temporal orchestration for approval workflows.
* Governance service split if required by scale/ownership.

## 11. Requirement-to-Design Traceability

| Requirement                         | Design Decision                                                        | Section |
| ----------------------------------- | ---------------------------------------------------------------------- | ------- |
| Multi-tenant isolation              | Mandatory tenant context in API, repos, Milvus filters, S3 prefixes    | 3, 4    |
| Governed ingestion                  | Profiler + policy gate with approved/quarantine/rejected states        | 3       |
| Vector store Milvus                 | Tenant-filtered vector retrieval with rerank pipeline                  | 3, 4    |
| Blob store S3                       | Tenant-scoped object pathing and lifecycle management                  | 3, 4, 7 |
| AI gateway Portkey + Qwen embedding | Adapter-based AI layer with sidecar embedding route                    | 2, 3    |
| Async ingestion before Temporal     | Queue orchestrator + workers + idempotency + DLQ                       | 3, 8    |
| Lightweight ingestion and search UIs | Two React + Vite frontends with dashboard/search boundaries             | 2, 5    |
| Security baseline                   | TLS, encryption at rest, hashed API keys, secrets manager              | 4       |
| Data residency ap-south-1           | Region lock in topology and governance checks                          | 3, 5    |
| SLO/NFR targets                     | Scaling policy, headroom, load-test gates, rollback gates              | 6, 9    |
| Cost attribution/optimization       | Tenant-tagged cost events + cache/routing/retention levers             | 7       |
| DR targets RPO/RTO                  | Snapshot policy and restore drill controls                             | 8       |
| CI/testing requirements             | Unit/integration/RAG evaluator/security gates in release flow          | 9       |

## 12. Open Risks and Unresolved Decisions

1. Managed reranker final vendor contract (SLA/price/legal) remains pending procurement sign-off.
2. API-key management guardrails need product/security confirmation: max active keys per tenant and mandatory dual-control for revoke in regulated tenants.
3. Hosted PostgreSQL/Milvus backup and incident-response obligations must be validated in provider contract against `RPO 24h` and `RTO 4h`.