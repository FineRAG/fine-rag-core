<p align="center">
  <strong>FineRAG — Enterprise-Grade Retrieval-Augmented Generation Platform</strong><br/>
  <em>Architected from scratch in Go · Multi-Tenant · Production-Hardened · Cloud-Native</em>
</p>

<p align="center">
  <code>Go 1.26</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>Milvus</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>PostgreSQL</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>Portkey AI Gateway</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>OpenRouter LLM</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>React + Vite</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>Docker</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>AWS</code>
</p>

---

## Live Application

<p align="center">
  <strong>Try it now:</strong><br/>
  <a href="https://dash-finer.shafeeq.dev"><strong>Ingestion Dashboard</strong></a> &nbsp;|&nbsp;
  <a href="https://finer.shafeeq.dev"><strong>Query UI</strong></a>
</p>

---

> **About this project** — I designed and built this system end-to-end as a solo architect/engineer to demonstrate how I approach production AI infrastructure. Every layer — from the hexagonal backend to the tenant-isolated vector pipeline to the streaming SSE search — reflects the kind of system-level thinking I bring to an AI architect role. The codebase is not a tutorial clone; it is an opinionated, security-first platform that could ship to production.

---

## Table of Contents

- [Why This Project Exists](#why-this-project-exists)
- [System Architecture](#system-architecture)
- [Core Platform Capabilities](#core-platform-capabilities)
- [Technical Deep Dive](#technical-deep-dive)
- [API Surface](#api-surface)
- [Security & Governance Model](#security--governance-model)
- [Observability & Operational Intelligence](#observability--operational-intelligence)
- [End-to-End Data Flow](#end-to-end-data-flow)
- [Tech Stack](#tech-stack)
- [Repository Structure](#repository-structure)
- [Local Quickstart](#local-quickstart)
- [Deployment](#deployment)

---

## Why This Project Exists

Most RAG implementations are demo-grade: single-tenant, no access control, naive chunking, no governance, and zero observability. They work in notebooks but collapse under production demands — **data isolation failures, uncontrolled PII leakage, retrieval quality degradation at scale, and token cost blowouts**.

FineRAG is my answer to the question: *"What does a RAG platform look like when an architect designs it for Day 2 operations, not just Day 1 demos?"*

This system addresses the hard problems that separate a proof-of-concept from a production AI platform:

- **Multi-tenant data isolation** enforced at every layer — middleware, database, vector store, object storage
- **Governed ingestion pipelines** with policy gates, PII detection, and deterministic audit trails
- **High-performance semantic retrieval** backed by Milvus with cross-encoder reranking
- **Real-time streaming answers** via Server-Sent Events with token-by-token LLM generation
- **Zero-trust security posture** with JWT auth, API key management, RBAC, and tenant-scoped access paths
- **Cost-optimized AI operations** through intelligent chunking, embedding caching strategies, and context window management

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           CLIENT LAYER                                      │
│  ┌─────────────────────┐          ┌─────────────────────┐                   │
│  │  Ingestion Dashboard │          │   Search Query UI    │                  │
│  │   React + Vite + TS  │          │   React + Vite + TS  │                  │
│  │  File Upload · Jobs  │          │  Streaming Answers   │                  │
│  │  Knowledge Bases     │          │  Citations · Traces  │                  │
│  └──────────┬──────────┘          └──────────┬──────────┘                   │
└─────────────┼────────────────────────────────┼──────────────────────────────┘
              │              REST + SSE         │
              ▼                                 ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         API GATEWAY LAYER (Go)                              │
│  ┌─────────┐ ┌──────────┐ ┌────────────┐ ┌────────────┐ ┌──────────────┐   │
│  │  CORS   │→│Request ID│→│ Access Log  │→│Rate Limiter│→│Tenant Context│   │
│  │Whitelist│ │Propagator│ │ Middleware  │ │ Token Bucket│ │  Enforcer    │   │
│  └─────────┘ └──────────┘ └────────────┘ └────────────┘ └──────────────┘   │
│                                  │                                          │
│                    ┌─────────────┼─────────────┐                            │
│                    ▼             ▼              ▼                            │
│             ┌───────────┐ ┌───────────┐ ┌────────────┐                      │
│             │   Auth    │ │ Ingestion │ │ Retrieval + │                      │
│             │  Handler  │ │  Handler  │ │  Search     │                      │
│             └───────────┘ └───────────┘ └────────────┘                      │
└─────────────────────────────────────────────────────────────────────────────┘
              │                    │              │
              ▼                    ▼              ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                       SERVICE LAYER (Domain Logic)                          │
│                                                                             │
│   ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐         │
│   │ Governance Svc   │  │  Ingestion Svc   │  │  Retrieval Svc   │         │
│   │ · Policy Gates   │  │ · Job Lifecycle  │  │ · Query Embed    │         │
│   │ · PII Detection  │  │ · PDF Extraction │  │ · Vector Search  │         │
│   │ · Residency Lock │  │ · Text Chunking  │  │ · Cross-Encoder  │         │
│   │ · Audit Events   │  │ · Embedding Gen  │  │   Reranking      │         │
│   │ · Quarantine     │  │ · Vector Upsert  │  │ · LLM Answer Gen │         │
│   └──────────────────┘  └──────────────────┘  │ · SSE Streaming  │         │
│                                                └──────────────────┘         │
└─────────────────────────────────────────────────────────────────────────────┘
              │                    │              │
              ▼                    ▼              ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                      ADAPTER LAYER (Ports & Adapters)                       │
│                                                                             │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐              │
│  │ PostgreSQL │ │   Milvus   │ │  AWS S3 /  │ │  Portkey   │              │
│  │  Adapter   │ │  Adapter   │ │   MinIO    │ │  Gateway   │              │
│  │            │ │            │ │  Adapter   │ │  Adapter   │              │
│  │· Tenants   │ │· Upsert    │ │· Presigned │ │· Embeddings│              │
│  │· Users     │ │· Search    │ │  Uploads   │ │· Chat/LLM  │              │
│  │· Jobs      │ │· Purge     │ │· Blob Get  │ │· Reranker  │              │
│  │· Audit     │ │· Tenant    │ │· Tenant    │ │· Streaming │              │
│  │· Migrations│ │  Filtering │ │  Scoped    │ │· Fallback  │              │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘              │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Architectural style**: Hexagonal Architecture (Ports & Adapters) — every external dependency is abstracted behind a Go interface defined in `internal/contracts/`. Adapters are swappable. The core domain has zero import dependencies on infrastructure.

---

## Core Platform Capabilities

### High-Performance Hybrid Ingestion Pipeline

FineRAG implements a **high-throughput, multi-stage pipeline** designed for IO-intensive production workloads, capable of processing **100K+ documents per day**. By leveraging Go’s lightweight concurrency (Goroutines) and native CGO bindings, the system achieves near-instantaneous parsing with minimal memory overhead.

- **Hybrid Extraction Architecture**: Supports two swappable parsing engines to balance speed and accuracy:
    - **Extractous-Go (Local)**: A high-speed, native Go-extractor for **DOC, DOCX, PPT, PPTX, and Images**. Optimized for memory-constrained environments (**< 500MB RAM**), it runs directly within the backend process. It provides **sub-second extraction** for standard enterprise documents, making it the ideal choice for high-volume POCs.
    - **IBM Docling (Remote)**: An optional high-precision engine for documents with extremely complex layouts (tables, multi-column charts). While slower than local extraction, it provides state-of-the-art layout understanding for specialized use cases.
- **Accuracy & Speed**:
    - **Native Speed**: By bypassing the Python interpreter and using Go, the ingestion pipeline reduces latency by up to **5x** compared to traditional RAG stacks.
    - **Robust Extraction**: Integrated OCR (via Tesseract) ensures text is recovered even from non-searchable PDFs and images.
- **Expanded Format Support**: Native parsing for **Microsoft Word**, **PowerPoint**, and **Common Image Formats (JPG, PNG)**.
- **Sliding-window chunking with configurable overlap** (default 900 chars / 30-word overlap) — designed to preserve semantic coherence across chunk boundaries.
- **Metadata-enriched vectors** — every chunk carries `object_key`, `source_uri`, `file_name`, `person_hint`, and MIME type through the entire pipeline.
- **Presigned upload flow** — client-side S3/MinIO uploads with server-generated presigned URLs, eliminating backend as a data bottleneck.
- **Job lifecycle state machine**: `queued → approved|quarantine|rejected → indexing → indexed|failed` with full audit trail.

### High-Performance Semantic Retrieval Engine

- **Milvus-powered vector search** with mandatory tenant-scoped filtering — cross-tenant data leakage is architecturally impossible.
- **Advanced Indexing Strategy**: The vector field utilizes Milvus `AUTOINDEX` (optimized for dense ANN search on Zilliz Cloud) with `COSINE` similarity.
- **O(1) Pre-filtering**: The `tenant_id` and `object_key` fields are indexed as scalar fields, enabling O(1) metadata filtering prior to vector search, significantly reducing latency and compute overhead.
- **Prefix-Based Purging**: The `object_key` is a first-class indexed field, allowing for efficient data purging by prefix (e.g., when a specific document or an entire tenant is removed via `POST /tenants/{id}/purge`).
- **Cross-encoder reranking** via direct **Cohere reranker-v4.0** integration for precision-tuned relevance scoring on top-K candidates.
- **LLM-grounded answer generation** with citation assembly — answers include source document references, not hallucinated claims.
- **Dual query modes**: synchronous JSON response OR **real-time SSE streaming** with token-by-token answer delivery, citation events, and trace metadata.
- **Embedding model failover** — automatic retry with configurable fallback model when primary embedding provider degrades.
- **Query intent scoring** — heuristic classification of query signals (education, qualification, experience) for domain-aware ranking.

### Real-Time Streaming Architecture (SSE)

Server-Sent Events are a first-class citizen, not an afterthought:

- **Search stream** (`POST /api/v1/search/stream`) — emits structured events: `top_vectors → token → citation → trace → done`
- **Job stream** (`GET /api/v1/ingestion/jobs/stream`) — live ingestion job status updates
- Implemented with Go's `http.Flusher` for zero-buffering latency
- Proper SSE protocol compliance with `text/event-stream` content type and `Cache-Control: no-cache`

### Multi-Tenant Isolation by Design

Tenant boundaries are not an access-control layer bolted on after the fact. They are **structural** — enforced at middleware, service, repository, vector store, and object storage layers:

| Layer | Isolation Mechanism |
|---|---|
| HTTP Middleware | `X-Tenant-ID` header required; requests without tenant context are rejected with `400/401` |
| PostgreSQL | Every query includes `WHERE tenant_id = $1` — enforced at the repository interface level |
| Milvus | Mandatory `tenant_id` scalar filter on every vector search — no global queries possible |
| S3 / MinIO | Object keys namespaced: `tenant_id/YYYYMMDD/filename` — prefix-scoped bucket policies |
| Audit | Every event tagged with `tenant_id` — full attribution chain |

---

## Technical Deep Dive

### Backend Architecture (Go)

The backend follows **strict hexagonal architecture** principles:

```
internal/
├── contracts/        # Port interfaces (zero infrastructure imports)
│   ├── repository.go # Data access contracts
│   ├── vector.go     # Vector store contracts
│   ├── gateway.go    # LLM/embedding gateway contracts
│   └── storage.go    # Object storage contracts
├── adapters/         # Infrastructure implementations
│   ├── gateway/portkey/   # Portkey AI gateway (embeddings, chat)
│   ├── reranker/cohere/   # Direct Cohere reranker-v4.0 adapter
│   └── vector/milvus/    # Milvus vector database adapter
├── services/         # Domain logic (no infrastructure knowledge)
│   ├── ingestion/    # Job lifecycle, profiling, policy enforcement
│   ├── retrieval/    # Search, rerank, answer generation
│   └── governance/   # PII checks, residency validation, audit
├── auth/             # JWT issuance, API key hashing (SHA-256)
├── middleware/       # HTTP middleware chain
├── ratelimit/        # Token-bucket rate limiter (per-tenant + global)
├── repository/       # PostgreSQL data access
├── runtime/          # Dependency assembly, config resolution
├── logging/          # Structured logging (zap)
└── telemetry/        # OpenTelemetry instrumentation
```

**Key design decisions:**
- **Dependency injection via constructor functions** — no DI framework, no magic. Dependencies are assembled in `backend/server.go` and flow downward through interfaces
- **Environment-driven configuration** with Docker secret file support (`_FILE` suffix convention)
- **Auto-migration on startup** — PostgreSQL schema evolves declaratively via numbered migration files
- **Bootstrap data seeding** — admin user and default tenant created on first launch
- **Graceful degradation** — stub adapters for vector store and gateway enable local development without cloud dependencies

### Data Model

```sql
-- Multi-tenant registry with plan tiers
tenant_registry (tenant_id, display_name, plan_tier, active, updated_at)

-- User management with hashed credentials
app_users (id, username, password_hash, api_key_hash, active)
user_tenants (user_id, tenant_id)  -- RBAC: many-to-many

-- Governed ingestion with full lifecycle tracking
ingestion_jobs (
    job_id, tenant_id, source_uri, checksum,
    status,    -- queued | approved | quarantine | rejected | indexing | indexed | failed
    stage,     -- cleanup | scanning | embedding | indexed
    processed_files, total_files, successful_files, failed_files,
    policy_code, policy_reason,  -- governance decision audit
    chunk_count, payload_bytes,
    submitted_at, updated_at
)

-- Immutable audit trail
audit_events (event_id, tenant_id, event_type, resource, actor, outcome, occurred_at, attributes JSONB)
```

### Authentication & Access Control

- **Dual auth modes**: Username/password OR API key — both produce JWT tokens
- **SHA-256 hashed credentials** — passwords and API keys stored as one-way hashes
- **JWT claims**: `{sub, uid, iat, exp}` with configurable TTL (default 8h)
- **User-to-tenant RBAC mapping** via join table — users see only their authorized tenants
- **API key revocation** support without token invalidation cascade

### Rate Limiting

- **Dual-tier token bucket**: per-tenant burst (default 4 req/sec) + global burst cap (default 600 req/sec)
- **Second-window counter** implementation — lightweight, no external Redis dependency
- Configurable via environment variables for per-deployment tuning

---

## API Surface

All endpoints require JWT authentication except login. Every mutating or query endpoint requires `X-Tenant-ID` and `X-Request-ID` headers.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/auth/login` | Authenticate (password or API key) → JWT |
| `GET` | `/api/v1/tenants` | List authorized tenants |
| `POST` | `/api/v1/tenants` | Create new tenant |
| `GET` | `/api/v1/knowledge-bases` | Knowledge bases grouped by source |
| `GET` | `/api/v1/tenants/{id}/vector-stats` | Vector count + storage metrics |
| `POST` | `/api/v1/uploads/presign` | Generate S3 presigned upload URLs |
| `POST` | `/api/v1/ingestion/jobs` | Submit ingestion job |
| `GET` | `/api/v1/ingestion/jobs` | List jobs (paginated) |
| `GET` | `/api/v1/ingestion/jobs/stream` | SSE: live job status stream |
| `POST` | `/api/v1/ingestion/jobs/{id}/retry` | Retry failed job |
| `POST` | `/api/v1/search` | Semantic search + LLM answer |
| `POST` | `/api/v1/search/stream` | SSE: streaming search with token-by-token answers |
| `POST` | `/api/v1/tenants/{id}/purge` | Purge all tenant data |
| `GET` | `/healthz` | Health check |

**Streaming search response events:**
```
data: {"type":"top_vectors", "topVectors":[{rank, score, snippet, sourceUri}]}
data: {"type":"token", "token":"The"}
data: {"type":"token", "token":" answer"}
data: {"type":"token", "token":" is..."}
data: {"type":"citation", "citation":{"id":"...", "title":"...", "uri":"..."}}
data: {"type":"trace", "trace":{"vectorLookupMs":42, "rerankMs":120, "ttftMs":380}}
data: {"type":"done", "citations":[...], "trace":{...}}
```

---

## Security & Governance Model

### Zero-Trust Security Posture

- **TLS in transit** for all client-to-API and service-to-external communication
- **Encryption at rest** for PostgreSQL and S3 objects
- **Docker secrets** for credential injection — no environment variable exposure
- **Hashed credential storage** (SHA-256) — no plaintext passwords anywhere in the system
- **Mandatory tenant context** — requests without `X-Tenant-ID` fail fast at the middleware layer
- **CORS whitelist enforcement** — origin validation on every request

### Governance Pipeline

Every document entering the system passes through a **deterministic governance gate** before indexing:

```
Upload → Profiler → PII Detection → Residency Check → Policy Decision
                                                            │
                                          ┌─────────────────┼─────────────────┐
                                          ▼                 ▼                 ▼
                                      APPROVED          QUARANTINE        REJECTED
                                      → Index            → Hold            → Deny
                                                          → Review
```

- **PII detection and redaction pathways** integrated into both ingestion and retrieval flows
- **Data residency enforcement** — region-locked processing (`ap-south-1`)
- **Deterministic policy decisions** — every governance outcome is auditable and reproducible
- **Immutable audit trail** — `audit_events` table captures auth outcomes, policy decisions, and admin actions with JSONB attributes

---

## Observability & Operational Intelligence

### Structured Logging

Every request is traced with structured log entries across the full lifecycle:

```
search.step.received → search.step.embedding → search.step.vector_lookup.start
→ search.step.vector_lookup.done → search.step.rerank → search.step.llm.start
→ search.step.llm.done → search.step.response
```

Fields include: `request_id`, `tenant_id`, `duration_ms`, `doc_count`, `vector_provider`, `model`, `token_count`

### OpenTelemetry Integration

- **OTLP exporter** for Grafana Cloud, Datadog, or any OTel-compatible backend
- **HTTP server telemetry** with request/response metrics
- **Configurable via environment** — `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`
- **Managed Prometheus + Grafana** for production dashboards

---

## End-to-End Data Flow

### Ingestion Pipeline

```bash
# 1. Authenticate
TOKEN=$(curl -sS -X POST "$BASE_URL/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","apiKey":"sk-..."}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')

# 2. Get tenant context
TENANT_ID=$(curl -sS "$BASE_URL/api/v1/tenants" \
  -H "Authorization: Bearer $TOKEN" | python3 -c 'import json,sys;print(json.load(sys.stdin)[0]["tenantId"])')

# 3. Presign upload URL (direct-to-S3, backend never touches the blob)
PRESIGN=$(curl -sS -X POST "$BASE_URL/api/v1/uploads/presign" \
  -H "Authorization: Bearer $TOKEN" -H "X-Tenant-ID: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"files":[{"name":"document.pdf","size":102400,"type":"application/pdf"}]}')

# 4. Upload to S3 via presigned URL
curl -X PUT "$(echo $PRESIGN | jq -r '.uploads[0].uploadUrl')" \
  --data-binary @document.pdf

# 5. Submit ingestion job (triggers governed pipeline)
curl -X POST "$BASE_URL/api/v1/ingestion/jobs" \
  -H "Authorization: Bearer $TOKEN" -H "X-Tenant-ID: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"sourceMode":"local","objectKeys":["..."]}'
```

### Semantic Search (Streaming)

```bash
# Real-time streaming search with LLM-generated answers
curl -N -X POST "$BASE_URL/api/v1/search/stream" \
  -H "Authorization: Bearer $TOKEN" -H "X-Tenant-ID: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"queryText":"What are the key compliance requirements?","topK":5}'

# Returns SSE stream:
# data: {"type":"top_vectors","topVectors":[...]}
# data: {"type":"token","token":"Based"}
# data: {"type":"token","token":" on"}
# data: {"type":"token","token":" the"}
# ...
# data: {"type":"done","citations":[...],"trace":{"vectorLookupMs":38,"rerankMs":95}}
```

---

## Tech Stack

| **Backend Runtime** | Go 1.26 | High-throughput concurrency, low GC latency, single-binary deployment |
| **Document Parser** | Extractous-Go | **High-speed local extraction** for DOC/PPT/Images (< 500MB RAM footprint) |
| **Secondary Parser**| IBM Docling | Optional high-precision layout understanding (remote API) |
| **Vector Database** | Milvus (Zilliz Cloud) | Enterprise-scale ANN search, billion-vector capacity, managed operations |
| **Relational Database** | PostgreSQL (Supabase) | Battle-tested OLTP, JSONB for flexible attributes, managed backups |
| **Object Storage** | AWS S3 / MinIO | Presigned uploads, tenant-scoped prefixes, lifecycle policies |
| **LLM Gateway** | Portkey AI | Multi-provider routing, fallback chains, rate limit management |
| **LLM Provider** | OpenRouter | Access to frontier models (GPT-4o, Claude, Llama) via single API |
| **Embedding Model** | NVIDIA Llama-Nemotron Embed | High-quality dense embeddings, free tier available |
| **Reranker** | Direct Cohere reranker-v4.0 | Precision reranking of top-K candidates |
| **Frontend** | React + TypeScript + Vite | Type-safe UI with fast HMR development |
| **Observability** | OpenTelemetry + Grafana Cloud | Vendor-neutral telemetry, managed dashboards |
| **Containerization** | Docker + Docker Compose | Reproducible builds, secret management, multi-service orchestration |
| **Logging** | Uber Zap | Structured, zero-allocation JSON logging |
| **Deployment** | AWS EC2 (ap-south-1) | rsync-based deploy with health checks |

---

## Repository Structure

```
enterprise-go-rag/
├── cmd/
│   ├── finerag-backend/        # Application entrypoint
│   └── bootstrap-reset/        # Admin credential reset utility
├── internal/
│   ├── contracts/              # Port interfaces (hexagonal core)
│   ├── adapters/               # Infrastructure adapters (Milvus, Portkey)
│   ├── services/               # Domain services (Ingestion, Retrieval, Governance)
│   ├── auth/                   # JWT + API key authentication
│   ├── middleware/             # HTTP middleware chain
│   ├── ratelimit/             # Token-bucket rate limiter
│   ├── repository/            # PostgreSQL data access
│   ├── runtime/               # Dependency assembly + config
│   ├── logging/               # Structured logging (zap)
│   └── telemetry/             # OpenTelemetry instrumentation
├── backend/                    # HTTP handlers, server setup, routing
├── migrations/                 # PostgreSQL schema (5 versioned migrations)
├── ingestion-dashboard-ui/     # React frontend: upload, jobs, knowledge bases
├── search-query-ui/           # React frontend: search, streaming answers, citations
├── test/                       # Integration + architecture tests
├── docs/                       # Architecture decisions, system design, requirements
├── scripts/                    # Deploy, migration, health check scripts
├── secrets/                    # Local credential files (gitignored)
├── docker-compose.yml          # Full stack orchestration
├── Dockerfile.backend          # Multi-stage Go build
└── go.mod                      # Go module dependencies
```

---

## Local Quickstart

### Prerequisites

- Docker + Docker Compose v2
- Go 1.26+ (for local builds/tests)
- Credential files in `secrets/` directory

### Launch

```bash
docker compose up -d --build
```

### Endpoints

| Service | Local | Production |
|---------|-------|------------|
| Backend API | `http://localhost:18080` | `https://api.finer.shafeeq.dev` |
| Ingestion Dashboard | `http://localhost:14173` | `https://dash-finer.shafeeq.dev` |
| Search Query UI | `http://localhost:14174` | `https://finer.shafeeq.dev` |

### Run Tests

```bash
go test ./internal/... ./test/... ./backend/...
```

### Stop

```bash
docker compose down
```

---

## Deployment

### Cloud Deploy (AWS EC2)

```bash
# One-command deploy with rsync + health checks
./scripts/deploy_sync_health.sh
```

### Deploy + Tunnel

```bash
# Deploy and open SSH tunnel for remote access
./run_stack_with_tunnel.sh
```

### Observability Setup

```bash
# Connect to Grafana Cloud OTLP
export OTEL_EXPORTER_OTLP_PROTOCOL="http/protobuf"
export OTEL_EXPORTER_OTLP_ENDPOINT="https://otlp-gateway-<zone>.grafana.net/otlp"
docker compose up -d --build
```

---

<p align="center">
  <em>Designed and built end-to-end by a single architect-engineer.<br/>
  Every design decision is intentional. Every abstraction earns its place.</em>
</p>
