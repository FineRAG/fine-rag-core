# Enterprise Go RAG

Enterprise Go RAG is a multi-tenant, governance-first Retrieval-Augmented Generation (RAG) platform designed to move teams from prototype to production quickly, without sacrificing security, compliance, or control.

Built in Go and designed for AWS deployments, it combines low-latency retrieval, policy-gated ingestion, strict tenant isolation, and practical operational knobs that platform teams need in real environments.

Keywords: enterprise RAG, multi-tenant RAG, governed AI pipeline, secure RAG architecture, compliance-ready RAG, production RAG in Go, low-latency retrieval, policy-based ingestion, data residency controls, tenant-isolated vector search.

## Why This Project Exists

Most RAG systems fail in production for predictable reasons:

- Weak data controls let poor or unsafe content into the index.
- Multi-tenant isolation is bolted on late, causing risk and rework.
- Performance falls apart under real traffic.
- Compliance and audit trails are afterthoughts.

Enterprise Go RAG addresses these failure points from day one with architecture-level controls and explicit reliability targets.

## Value Proposition

- Reach production faster with a pre-defined architecture, service boundaries, and deployment model.
- Avoid GIGO (Garbage In, Garbage Out) with governed ingestion gates before indexing.
- Protect tenant boundaries across API, storage, and retrieval paths.
- Meet enterprise expectations on performance and availability with explicit SLO targets.
- Operate with clear knobs for cost, throughput, quality, and policy strictness.

## Built For Production Speed

This project is intentionally designed to reduce time-to-production:

- REST-first service boundaries for quick integration.
- Queue-based asynchronous ingestion in v1 for immediate operational viability.
- Containerized runtime model on EC2 with rsync-based deployment workflow.
- Clear v1 scope with a staged path for v1.1 enhancements.

Baseline performance and reliability targets:

- 300 RPS steady retrieval load.
- 600 RPS burst cap.
- p95 retrieval latency <= 800 ms.
- 99.9% monthly availability objective.

## Control Knobs You Can Actually Use

Enterprise RAG teams need operational controls, not fixed pipelines. This architecture includes practical levers:

- Throughput and quota controls: per-tenant default quota at 2 RPS steady / 4 RPS burst, plus a global 600 RPS burst cap.
- Retrieval quality and cost controls: managed reranker routing by tenant tier, top-K rerank bounds with timeout fallback, and embedding cache keys using `(tenant_id, content_hash, model_version)`.
- Ingestion and lifecycle controls: policy outcomes (`approved`, `quarantine`, `rejected`), lifecycle retention classes, and queue backpressure by worker lag and job age.
- Operations controls: autoscaling by CPU, queue depth, and latency indicators, with tenant-level budget visibility and resilient upstream retry/circuit-breaker behavior.

## How GIGO Is Prevented

GIGO is addressed with an explicit governed-ingestion flow before content reaches the vector index:

1. Upload request is authenticated and quota-checked in tenant context.
2. Content is profiled and evaluated by governance policy gates.
3. PII redaction and policy checks determine index eligibility.
4. Only approved artifacts proceed to chunking and embedding.
5. Metadata and audit records persist policy outcomes for traceability.

This prevents unvetted, low-quality, or policy-violating data from silently degrading retrieval quality.

## Multi-Tenant by Design, Not by Convention

Tenant isolation is enforced across all critical layers:

- API/middleware: mandatory tenant context; fail-fast on missing context.
- PostgreSQL: tenant ownership predicates in repository paths.
- Milvus: mandatory `tenant_id` filter at query time.
- MinIO: tenant-scoped object prefixes and policy boundaries.
- Testing: isolation-focused integration tests for ingestion and retrieval leakage.

## Flexible Interfaces and Integration Boundaries

The platform is built around clear contracts and extensible boundaries:

- REST-first APIs for broad compatibility.
- Service boundaries for ingestion, retrieval, governance, and adapters.
- AI adapter abstraction through gateway pattern (Portkey in v1 design).
- Sidecar embedding route with Qwen3-Embedding-4B.
- Managed reranker API integration for relevance upgrades.

This keeps implementation adaptable while preserving strong architectural guardrails.

## Compliance and Security Focus Out of the Box

Enterprise Go RAG starts with governance and security controls as core capabilities:

- PII redaction before indexing and before model/reranker calls.
- Data residency lock to AWS `ap-south-1`.
- TLS in transit.
- Encryption at rest for database and object storage.
- Hashed and salted tenant API keys (revoke-only lifecycle in v1).
- Audit records for auth outcomes, policy outcomes, and privileged actions.
- Disaster recovery targets: RPO 24h, RTO 4h.

## Architecture Highlights

Core runtime and platform components:

- API gateway for auth, rate limiting, tenant context, routing, and response shaping.
- Ingestion orchestrator and worker pool for async document processing.
- Retrieval service for embedding, tenant-filtered search, reranking, and citations.
- Governance module/service for policy and compliance decisions.
- PostgreSQL for tenant registry, metadata, and audit records.
- MinIO for blob storage.
- Milvus for vector search.
- Prometheus + Grafana + OpenTelemetry for observability.

## Observability and Cost Accountability

The architecture includes enterprise telemetry priorities:

- RED metrics for critical API and retrieval paths.
- TTFT and end-to-end retrieval timing.
- Stage-level latency breakdowns for embed/search/rerank/generate.
- Tenant-level token and cost attribution model.
- Budget alarms and anomaly detection hooks.

## Developer and Delivery Model

- Local development and AI-assisted authoring in VS Code.
- Build/deploy path designed for EC2 container rollout.
- Structured quality gates across unit, integration, performance, and security checks.
- Clear phase plan for v1 and v1.1 evolution.

## Run and Validate Tests

Use this sequence before merge or deployment.

1. Run focused gates used by the current task packs:

```bash
go test ./... -run Contract -count=1
go test ./... -run Architecture -count=1
go test ./... -run TenantContext -count=1
go test ./... -run Isolation -count=1
```

2. Run complete suite:

```bash
go test ./...
```

3. Optional: run only centralized tests under the top-level test folder:

```bash
go test ./test/...
```

## Ingestion Dashboard UI (E4-T1)

The repository now includes `ingestion-dashboard-ui/` (React + Vite) for tenant session bootstrap, ingestion submission/status, and API-key create/revoke controls.

1. Local development:

```bash
cd ingestion-dashboard-ui
npm ci
VITE_INGESTION_API_BASE_URL=http://localhost:8080 npm run dev
```

2. Frontend quality gates:

```bash
cd ingestion-dashboard-ui
npm run lint
npm run test
npm run build
```

3. Containerized serving path:

```bash
docker compose up -d ingestion-dashboard-ui
```

The UI is served at `http://localhost:14173` when the compose service is running.

## Search Query UI (E4-T2)

The repository includes `search-query-ui/` (React + Vite) for tenant-scoped query input, SSE answer streaming, citation rendering, and trace metadata visibility.

1. Local development:

```bash
cd search-query-ui
npm ci
VITE_SEARCH_API_BASE_URL=http://localhost:8080 npm run dev
```

2. Frontend quality gates:

```bash
cd search-query-ui
npm run lint
npm run test
npm run build
```

3. Containerized serving path:

```bash
docker compose up -d search-query-ui
```

The UI is served at `http://localhost:14174` when the compose service is running.

Validation is OK when:

- All commands exit with code `0`.
- `go test` output shows `ok` for applicable packages.
- No package shows `FAIL`.

If validation is not OK, use this next-step workflow:

1. Re-run only the failing test package to isolate quickly:

```bash
go test ./test/<failing-package> -count=1 -v
```

2. Re-run by test name for faster debug cycles:

```bash
go test ./... -run <FailingTestName> -count=1 -v
```

3. Fix the issue, then run the full validation sequence again.
4. Only proceed to deployment after the full suite is green.
5. If the issue is environment-related (for example Docker/EC2/network), resolve infra first, then re-run tests and health checks.

## Who This Is For

- Platform teams building multi-tenant AI search and answer systems.
- Enterprises that require data governance and residency controls.
- Engineering organizations that need fast delivery with compliance confidence.
- Teams replacing fragile prototype RAG stacks with production architecture.

## Current Status

This repository contains the foundational architecture and implementation tracks for milestone v1, with clear upgrade paths for v1.1 orchestration expansion.

Go baseline:

- `go 1.26`
- `toolchain go1.26.1`

## Source of Truth

The statements in this README are aligned to:

- `docs/distilled_requirements.md`
- `docs/system_design.md`
- `docs/architecture/dependency-map.md`

## Enterprise RAG, Without Compromise

Enterprise Go RAG is built for teams that need to ship quickly and stay in control: fast retrieval, governed ingestion, strict isolation, flexible integration surfaces, and compliance-ready defaults.
