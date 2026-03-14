# FineR (Fine-RAG) 🚀

`#EnterpriseAI` `#RAGPlatform` `#Go` `#SecurityByDefault` `#HighThroughput`

FineR is an enterprise RAG platform built in Go for teams that need speed, control, and trust in production AI systems.

## Why FineR? 🧠


`[PAIN-KILLER]` `[#LessRework]` `[#FasterRollout]`

- 🔁 No more rebuilding pipelines every quarter due to data quality drift.
- 🧹 No more debugging weak retrieval caused by unclean document ingestion.
- 🔐 No more retrofitting security controls after launch.
- 🧱 No more tenant-boundary incidents from ad-hoc architecture.

`[OPERATING MODEL]`

- 🧼 Clean and normalize data before embedding.
- 🏷️ Persist metadata to power filtering and routing.
- 🛡️ Enforce tenant and access boundaries at query time.
- 📊 Keep governance, traceability, and observability in the request path.

## Business Impact 💼

`[#TCO]` `[#ROI]` `[#ProductionReady]`

- ⚡ Faster rollout with one stack for ingestion, retrieval, and secure query APIs.
- 🧰 Less rework through standardized cleaning, chunking, embedding, and indexing.
- 🎯 Better answer quality with metadata-aware retrieval and grounded generation.
- 🛡️ Lower risk with policy checks and layered controls from day 1.
- 💸 Better cost control via cleaner context and reduced token waste.

## Performance and Scale ⚙️

`[#BlazingFast]` `[#Scalable]` `[#Streaming]`

- 🏎️ Go backend optimized for high-throughput concurrent workloads.
- 🧠 Milvus-backed semantic retrieval at enterprise scale.
- 📦 Chunked ingestion pipeline for large document sets.
- 🌊 Streaming search API for low-latency UX.
- ☁️ Container-native architecture for cloud and on-prem deployment.

## Security, Governance, and Data Protection 🔒

`[#ZeroLeakage]` `[#RBAC]` `[#ABAC]` `[#PII]` `[#RegulatedData]`

- 🧱 Engineered layered security and governance model: tenant-scoped access paths, RBAC/ABAC enforcement, and automated PII redaction pathways to ensure zero-leakage compliance for regulated data.
- 🏢 Multi-tenant isolation with mandatory tenant context.
- 🧾 Governance policies for restricted and residency-sensitive data.
- 🧭 Metadata-driven filtering to prevent cross-domain leakage.
- 👤 Built-in PII handling paths with redaction/anonymization policy hooks.
- 📚 Audit-friendly traces and request-level diagnostics.

## Architecture Snapshot 🧩

`[#Modular]` `[#EnterpriseStack]`

- 🛠️ Backend: Go (`cmd/finerag-backend`)
- 🧠 Vector Store: Milvus
- 🗃️ Object Store: AWS S3
- 🐘 Database: PostgreSQL
- 🌐 Model Gateway: Portkey
- 🤖 LLM/Embeddings: OpenRouter-configured models
- 📤 Ingestion UI: `ingestion-dashboard-ui`
- 🔎 Search UI: `search-query-ui`

## Local Quickstart in Minutes 🚦

`[#DevReady]` `[#OneCommand]`

### Prerequisites

- 🐳 Docker + Docker Compose plugin
- 🧪 Go toolchain (for local tests/build)
- 🔐 `secrets/*.txt` available
- ☁️ AWS S3 bucket access from the runtime environment

### Launch

```bash
docker compose -f docker-compose.yml up -d --build
```

### Endpoints

- 🔌 Backend API: `http://localhost:18080`
- 📤 Ingestion Dashboard: published by `docker-compose.yml`
- 🔎 Search Query UI: published by `docker-compose.yml`

Observability for deployed environments is expected to come from managed Grafana and managed Prometheus rather than containers in this repo.
Object storage for deployed environments is expected to come from AWS S3.

### Stop

```bash
docker compose -f docker-compose.yml down
```

## Upload and Query Flow 🛤️

`[#PresignedUpload]` `[#GovernedIngestion]` `[#SemanticSearch]`

### 1. Login and get token 🔑

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

### 2. Resolve tenant 🏷️

```bash
TENANT_ID=$(curl -sS "$BASE_URL/api/v1/tenants" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Request-ID: $REQ_ID" | \
  python3 -c 'import json,sys;d=json.load(sys.stdin);print(d[0]["tenantId"])')
```

### 3. Presign upload URL 📎

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

### 4. Upload + create ingestion job 📥

```bash
curl -sS -X PUT "$UPLOAD_URL" -H "Content-Type: application/pdf" --data-binary @"$FILE_PATH" > /dev/null

curl -sS -X POST "$BASE_URL/api/v1/ingestion/jobs" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "X-Request-ID: $REQ_ID" \
  -d "{\"sourceMode\":\"local\",\"sourceUri\":\"local://$FILE_NAME\",\"objectKeys\":[\"$OBJECT_KEY\"],\"localItems\":[{\"name\":\"$FILE_NAME\",\"size\":$FILE_SIZE,\"type\":\"application/pdf\",\"relativePath\":\"$FILE_NAME\"}]}"
```

### 5. Query search API 🔍

```bash
curl -sS -X POST "$BASE_URL/api/v1/search" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "X-Request-ID: $REQ_ID" \
  -d '{"queryText":"what is shafeeq email address?","topK":5}'
```

### 6. Query stream API (SSE) 🌊

```bash
curl -sS -N -X POST "$BASE_URL/api/v1/search/stream" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Tenant-ID: $TENANT_ID" \
  -H "X-Request-ID: $REQ_ID" \
  -d '{"queryText":"what is shafeeq email address?"}'
```

## Governance and Restricted Data Controls 🛂

`[#ComplianceReady]` `[#PolicyGates]`

- 📌 Ingestion policy gates for residency, PII, and data handling checks.
- 🧾 Deterministic governance decisions with audit sink support.
- 🎯 Metadata and intent-aware ranking for restricted scopes.
- 🧹 Safer context construction to avoid unreadable/noisy payload injection into generation.

## Observability and Ops 📡

`[#TraceEverything]` `[#OperateWithConfidence]`

- 🪵 Structured logs across ingestion, embedding, retrieval, rerank, and generation.
- 🔎 Debug traces for query, retrieved vectors, LLM input, and final output.
- 📈 Managed Prometheus + Grafana visibility.

Grafana Cloud OTLP setup for the backend:

```bash
export OTEL_EXPORTER_OTLP_PROTOCOL="http/protobuf"
export OTEL_EXPORTER_OTLP_ENDPOINT="https://otlp-gateway-<zone>.grafana.net/otlp"
export OTEL_EXPORTER_OTLP_HEADERS="Authorization=Basic%20<base64-encoded-instance-id-and-token>"

docker compose -f docker-compose.stack.yml up -d --build
```

The backend container forwards those env vars directly and emits HTTP server telemetry over OTLP when `OTEL_EXPORTER_OTLP_ENDPOINT` is set.

## Remote Deploy + Local Tunnel 🌍

`[#EC2]` `[#Tunnel]`

```bash
./run_stack_with_tunnel.sh
```

Variant:

```bash
./run_stack_with_tunnel.sh --skip-deploy
```

## Positioning Summary 🏁

`#Fast` `#Secure` `#Governed` `#EnterpriseGrade`

FineR is not a demo-only RAG starter kit. It is an enterprise operating model for secure, high-throughput, governed retrieval and answer generation.

If your team needs rapid launch, strict data protection, and scalable AI search without recurring rework, FineR is built for that mandate.
