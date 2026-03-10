# FineR (Fine-RAG)

FineR is an enterprise RAG platform built in Go for teams that need speed, control, and trust in production AI systems.

It is designed to reduce rework across data engineering, platform, security, and AI teams by combining governed ingestion, high-throughput retrieval, and strict access controls in one stack.

## Why Enterprises Choose FineR

FineR is built to eliminate common enterprise pain:

- Rebuilding pipelines every quarter because data quality drifts.
- Spending engineering time debugging weak retrieval caused by unclean documents.
- Retrofitting security controls after launch.
- Handling incidents caused by tenant or policy boundary mistakes.

FineR addresses this with a single operational model:

- Clean and normalize data before embedding.
- Persist metadata that improves filtering and routing.
- Enforce tenant and access boundaries at query time.
- Keep governance, traceability, and observability in the request path.

## Business Impact

- Faster rollout: one stack for ingestion, retrieval, and secure query APIs.
- Less rework: standardized pipeline for cleaning, chunking, embedding, and indexing.
- Better answer quality: metadata-aware retrieval with rerank support and grounded generation.
- Lower risk: policy checks and protection controls are built in from day 1.
- Better cost control: reduced wasted tokens through cleaner context and stricter retrieval inputs.

## Built for Performance and Scale

- Go backend optimized for concurrent, high-throughput request handling.
- Milvus-backed vector retrieval for scalable semantic search.
- Chunked ingestion pipeline built for large document volumes.
- Streaming search API for responsive UX under load.
- Container-native architecture for horizontal scaling in cloud or on-prem environments.

## Trust, Security, and Data Protection by Default

- Multi-tenant isolation with mandatory tenant context.
- Layered access model with RBAC and ABAC-ready enforcement points.
- Governance policies for restricted data and residency-sensitive sources.
- Metadata-driven filtering to prevent cross-domain leakage.
- Built-in PII handling path with redaction/anonymization policy hooks.
- Audit-friendly traces and request-level diagnostics.

## Architecture Snapshot

- Backend: Go (`cmd/finerag-backend`)
- Vector store: Milvus
- Object store: MinIO
- Database: PostgreSQL
- Model gateway: Portkey
- LLM/Embedding providers: OpenRouter-configured models
- Ingestion UI: `ingestion-dashboard-ui` for governed upload/index flow
- Search UI: `search-query-ui` for search and stream validation

## Start the Stack Locally

### Prerequisites

- Docker and Docker Compose plugin
- Go toolchain (for local tests/build)
- `secrets/*.txt` files present (already expected by compose)

### Launch

```bash
docker compose -f docker-compose.yml up -d --build
```

Core local endpoints:

- Backend API: `http://localhost:18080`
- Ingestion Dashboard: `http://localhost:14173`
- Search Query UI: `http://localhost:14174`
- MinIO API: `http://localhost:19000`
- MinIO Console: `http://localhost:19001`
- Grafana: `http://localhost:13000`
- Prometheus: `http://localhost:19090`

To stop:

```bash
docker compose -f docker-compose.yml down
```

## Upload and Query in Minutes

### 1. Login and get token

```bash
BASE_URL=http://localhost:18080
REQ_ID=req-local-$(date +%s)
USERNAME=$(cat secrets/finerag_bootstrap_admin_username.txt)
API_KEY=$(cat secrets/finerag_bootstrap_admin_api_key.txt)

TOKEN=$(curl -sS -X POST "$BASE_URL/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -H "X-Request-ID: $REQ_ID" \
  -d "{\"username\":\"$USERNAME\",\"apiKey\":\"$API_KEY\"}" | \
  python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')
```

### 2. Resolve tenant

```bash
TENANT_ID=$(curl -sS "$BASE_URL/api/v1/tenants" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Request-ID: $REQ_ID" | \
  python3 -c 'import json,sys;d=json.load(sys.stdin);print(d[0]["tenantId"])')
```

### 3. Presign upload

```bash
FILE_PATH=./Shafeeq-Resume-Mar-2026.pdf
FILE_NAME=$(basename "$FILE_PATH")
FILE_SIZE=$(wc -c < "$FILE_PATH" | tr -d ' ')

PRESIGN_JSON=$(curl -sS -X POST "$BASE_URL/api/v1/uploads/presign" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "X-Request-ID: $REQ_ID" \
  -d "{\"files\":[{\"name\":\"$FILE_NAME\",\"size\":$FILE_SIZE,\"type\":\"application/pdf\",\"relativePath\":\"$FILE_NAME\"}]}")

UPLOAD_URL=$(printf %s "$PRESIGN_JSON" | python3 -c 'import json,sys;j=json.load(sys.stdin);print(j["uploads"][0]["uploadUrl"])')
OBJECT_KEY=$(printf %s "$PRESIGN_JSON" | python3 -c 'import json,sys;j=json.load(sys.stdin);print(j["uploads"][0]["objectKey"])')
```

### 4. Upload and create ingestion job

```bash
curl -sS -X PUT "$UPLOAD_URL" -H "Content-Type: application/pdf" --data-binary @"$FILE_PATH" > /dev/null

curl -sS -X POST "$BASE_URL/api/v1/ingestion/jobs" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "X-Request-ID: $REQ_ID" \
  -d "{\"sourceMode\":\"local\",\"sourceUri\":\"local://$FILE_NAME\",\"objectKeys\":[\"$OBJECT_KEY\"],\"localItems\":[{\"name\":\"$FILE_NAME\",\"size\":$FILE_SIZE,\"type\":\"application/pdf\",\"relativePath\":\"$FILE_NAME\"}]}"
```

### 5. Search

```bash
curl -sS -X POST "$BASE_URL/api/v1/search" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "X-Request-ID: $REQ_ID" \
  -d '{"queryText":"what is shafeeq email address?","topK":5}'
```

### 6. Stream search (SSE)

```bash
curl -sS -N -X POST "$BASE_URL/api/v1/search/stream" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "X-Request-ID: $REQ_ID" \
  -d '{"queryText":"what is shafeeq email address?"}'
```

## Governance and Restricted Data Controls

FineR is designed for policy-sensitive enterprise environments:

- Ingestion policy gates for residency, PII and data handling checks.
- Deterministic governance decisions with audit sink support.
- Metadata and intent-aware ranking paths to improve precision for restricted scopes.
- Safer context construction to avoid passing unreadable/noisy payloads into generation.

## Observability and Operations

- Structured logs for each stage: ingestion, embedding, retrieval, rerank, and generation.
- Debug traces for original query, retrieved vectors, LLM input, and final answer output.
- Prometheus and Grafana included for runtime visibility.

## Optional Remote Deploy and Local Tunnel

To deploy/sync to EC2 and expose both UIs locally:

```bash
./run_stack_with_tunnel.sh
```

Common variant:

```bash
./run_stack_with_tunnel.sh --skip-deploy
```

## Positioning Summary

FineR is not a demo-only RAG kit. It is an enterprise operating model for secure, high-throughput, governed AI retrieval and answer generation.

If your teams need a platform that is fast to launch, safe by default, and scalable without recurring rework, FineR is built for that exact mandate.
