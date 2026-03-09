# System Design Addendum - Milvus-First VDB Portability and Portkey Integration

Date: 2026-03-09
Status: Draft for implementation planning
Input: `docs/distilled_requirements_vdb_portkey.md`
Related baseline: `docs/system_design.md`

## 1. Design Summary

This addendum defines an implementation-ready extension to the current Go-RAG architecture for two streams:

1. Milvus-first vector store integration with provider-agnostic adapters.
2. Portkey gateway integration for retrieval/generation model calls aligned to existing `internal/contracts` and `internal/services/retrieval` boundaries.

Primary outcome:

- Retrieval and ingestion business logic continue depending on existing contracts (`VectorSearcher`, `VectorIndex`, `Reranker`) while runtime wiring selects concrete providers by config.
- Milvus and Portkey are v1 defaults.
- Initialization must fail fast on invalid provider config; runtime fallback is explicit and deterministic where required.

## 2. Architecture Overview and Boundaries

### 2.1 Current Stable Boundaries (already in repo)

- `internal/contracts/contracts.go`
: owns service-facing interfaces (`VectorIndex`, `VectorSearcher`, `EmbeddingProvider`, `Reranker`) and retrieval contracts (`RetrievalQuery`, `RetrievalResult`, `RetrievalTrace`).
- `internal/services/retrieval/service.go`
: deterministic retrieval orchestration, tenant filtering, rerank timeout and circuit-breaker fallback behavior.
- `internal/services/ingestion/service.go`
: async worker path that embeds chunks and upserts vectors via contracts.
- `internal/runtime/wiring.go`
: composition point for runtime wiring (currently governance and metadata services).

### 2.2 New Addendum Boundaries

- Keep all provider-specific SDK logic out of service packages.
- Introduce adapter packages under `internal/adapters/...` and provider selection in runtime wiring/config.
- Preserve existing retrieval and ingestion method signatures in v1.

### 2.3 Proposed Adapter Layer Structure

- `internal/adapters/vector/`
: provider-agnostic vector adapter composition and error normalization.
- `internal/adapters/vector/milvus/`
: Milvus `VectorSearcher` + `VectorIndex` implementation.
- `internal/adapters/vector/stub/`
: non-Milvus contract-compliant test/stub adapter for portability tests.
- `internal/adapters/gateway/`
: AI gateway boundary for model calls (Portkey-first), request metadata propagation, resiliency policies.
- `internal/adapters/gateway/portkey/`
: Portkey implementation.

Future work (not required for this stream):

- Additional production VDB providers (`pgvector`, `opensearch`, `weaviate`) under `internal/adapters/vector/<provider>/` after first portability baseline is complete.

## 3. Proposed Interfaces and Adapter Contracts

## 3.1 Existing Contracts (must remain source-compatible)

No breaking changes to these interfaces in v1:

- `VectorIndex.Upsert(ctx, records)`
- `VectorSearcher.Search(ctx, tenantID, queryText, topK)`
- `Reranker.Rerank(ctx, req)`

These contracts are already consumed by:

- `internal/services/ingestion/service.go`
- `internal/services/retrieval/service.go`

## 3.2 Additive Interfaces (new, provider-neutral)

Add new interfaces to `internal/contracts/contracts.go` as additive types only:

```go
// ProviderErrorCategory is normalized and stable across providers.
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

func (e ProviderError) Error() string { ... }
func (e ProviderError) Unwrap() error { ... }

// AIGateway is a provider-agnostic boundary for retrieval/generation model calls.
type AIGateway interface {
    Rerank(ctx context.Context, metadata RequestMetadata, req RerankRequest) ([]RerankCandidate, GatewayCallTrace, error)
    Generate(ctx context.Context, metadata RequestMetadata, req GenerationRequest) (GenerationResult, GatewayCallTrace, error)
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
```

Notes:

- `GenerationRequest/GenerationResult` are future-safe additions if generation code is introduced later; this stream can implement only rerank path first.
- Retrieval service can continue using `Reranker` in v1 by wrapping `AIGateway` as a `Reranker` adapter to avoid constructor churn.

## 3.3 Adapter Composition Pattern

- `milvus.Adapter` implements both `contracts.VectorSearcher` and `contracts.VectorIndex`.
- `portkey.RerankerAdapter` implements `contracts.Reranker` and internally uses gateway client + metadata propagation.
- Optional `gateway.RerankerWithFallback` decorator handles retry/circuit/fallback and returns deterministic reason strings for `RetrievalTrace.FallbackReason`.

## 4. Runtime Configuration and Provider Selection

## 4.1 Config Model

Add provider config loaders in `internal/runtime` similar to existing DB/queue loaders:

- `internal/runtime/vector_config.go`
- `internal/runtime/gateway_config.go`

Environment model (names aligned with existing `FINE_RAG_*` convention):

- `FINE_RAG_VECTOR_PROVIDER=milvus|stub|<future>`
- `FINE_RAG_MILVUS_ENDPOINT`
- `FINE_RAG_MILVUS_DATABASE`
- `FINE_RAG_MILVUS_COLLECTION`
- `FINE_RAG_MILVUS_USERNAME` (optional if token auth is used)
- `FINE_RAG_MILVUS_PASSWORD` or secret reference injected at runtime
- `FINE_RAG_MILVUS_TLS=true|false` (default `true`)
- `FINE_RAG_GATEWAY_PROVIDER=portkey|stub`
- `FINE_RAG_PORTKEY_BASE_URL`
- `FINE_RAG_PORTKEY_API_KEY` (runtime-injected secret)
- `FINE_RAG_GATEWAY_TIMEOUT`
- `FINE_RAG_GATEWAY_RETRY_MAX`
- `FINE_RAG_GATEWAY_CIRCUIT_FAILURE_THRESHOLD`
- `FINE_RAG_GATEWAY_FALLBACK_MODE=fail_closed|direct_allowlist|retrieval_only`

## 4.2 Provider Factory and Fail-Fast Wiring

Add factory wiring in `internal/runtime/wiring.go` (or a new `internal/runtime/providers.go`):

- Validate config on startup.
- Unknown provider -> startup error with explicit message.
- Missing required config for selected provider -> startup error.
- No silent fallback to unspecified providers.

Pseudo-flow:

1. Load and validate vector config.
2. Build vector adapter (`milvus` default).
3. Load and validate gateway config.
4. Build Portkey reranker adapter.
5. Inject into retrieval and ingestion services via existing contracts.

## 5. Failure Modes, Resiliency, and Fallback Behavior

## 5.1 Failure Matrix

- Vector provider init failure
: fail fast at startup (`FR-VDB-007`, `DEP-003`).
- Vector search timeout/unavailable
: return normalized provider error (`timeout`/`unavailable`) and preserve tenant-safe error text.
- Vector upsert failure in ingestion worker
: existing retry + DLQ flow remains authoritative (`internal/services/ingestion/service.go`).
- Portkey timeout/rate-limit
: retry up to configured budget, then deterministic fallback.
- Portkey auth failure
: categorize as `unauthorized`, no secret leakage in logs.
- Circuit open on rerank path
: existing retrieval fallback to ranked-by-search score remains.

## 5.2 Deterministic Fallback Policy

Given open requirement decision `OQ-002`, implement fallback as configurable with explicit trace reason:

- `fail_closed`
: return retrieval error if rerank/generation path depends on gateway.
- `direct_allowlist`
: fallback to direct provider only for approved tenant tiers/providers.
- `retrieval_only`
: skip generation/rerank and return retrieval result with `Trace.FallbackReason` populated.

Default recommendation for v1:

- Retrieval rerank path: `retrieval_only` to preserve availability.
- Generation path (when added): `fail_closed` unless security approves direct fallback.

## 6. Tenant Isolation, Security, and Compliance Controls

## 6.1 Tenant Isolation Controls

- Every vector search/index call keeps explicit tenant argument and enforces tenant filter in provider query.
- Defense-in-depth: retrieval service keeps existing post-query tenant filtering.
- Portkey metadata for every gateway call includes `tenant_id` and `request_id` from `RequestMetadata`.

## 6.2 Secrets and Credential Handling

- Credentials only from environment/secret manager injection at runtime (`SEC-001`, `GOV-003`).
- Do not print raw API keys/passwords in errors/logs.
- Add redaction helpers in runtime config (`Redacted()` pattern similar to `DatabaseConfig.RedactedURL()`).
- Credential rotation supported without business-logic changes by externalized config.

## 6.3 Compliance and Audit

- Add audit event emission for gateway fallback and denial events with non-sensitive attributes:
  - `tenant_id`, `request_id`, `provider`, `status`, `fallback_reason`, `policy_code` (if applicable).
- Keep governance/residency checks unaffected by adapter refactor.

## 7. Testing Strategy

## 7.1 Unit Tests

- `internal/adapters/vector/milvus/*_test.go`
: tenant filter enforcement, error normalization mapping, timeout behavior.
- `internal/adapters/gateway/portkey/*_test.go`
: metadata propagation, retries, circuit-breaker transitions, redaction-safe logs.
- `internal/runtime/*_config_test.go`
: env parsing, validation failures, redaction.

## 7.2 Contract Tests

- Extend `test/contracts/contract_test.go` with adapter compliance tests:
  - Milvus/stub both satisfy `VectorSearcher` and `VectorIndex` behavior contracts.
  - Unknown provider config fails wiring deterministically.

## 7.3 Integration Tests

- Extend `test/retrieval/`:
  - rerank fallback reason assertion on gateway timeout.
  - circuit breaker open behavior with deterministic fallback.
- Extend `test/ingestion/`:
  - vector upsert failure -> retry/DLQ path remains intact through adapter boundary.
- Preserve and run existing governance and retrieval deterministic tests.

## 7.4 Quality Gates

- Required:
  - `go test ./... -run 'Retrieval|TenantFilter|Governance|PII|Residency' -count=1`
  - `scripts/securitygov_review.sh 'PolicyGate|Governance|PII|Residency|Retrieval|TenantFilter'`
- Add secret scan checks for banned env key values in docs/tests.

## 8. Rollout and Migration Plan

## Phase 0: Scaffolding (no behavior change)

- Add new adapter packages and runtime config structs.
- Keep existing retrieval/ingestion constructors untouched.

## Phase 1: Milvus Adapter Extraction

- Implement Milvus adapter under `internal/adapters/vector/milvus` and wire through existing contracts.
- Add stub vector adapter for portability test coverage.
- Verify behavior parity against current retrieval and ingestion tests (`MIG-002`, `MIG-004`).

## Phase 2: Portkey Rerank Adapter Integration

- Implement Portkey-backed `Reranker` adapter and plug into `DeterministicRetrievalService`.
- Add retry/timeout/circuit decorators and trace fallback reason.
- No frontend API contract changes.

## Phase 3: Operational Hardening

- Add metrics for latency/provider status/token usage fields.
- Validate secret rotation runbook and failure drills.

Rollback path:

- Vector provider rollback via `FINE_RAG_VECTOR_PROVIDER=stub` (or previous stable provider) without changing retrieval/ingestion service code.
- Gateway rollback via `FINE_RAG_GATEWAY_PROVIDER=stub` and fallback mode policy.

## 9. Explicit Implementation Impact (Likely Files/Packages)

Likely new files:

- `internal/adapters/vector/adapter.go`
- `internal/adapters/vector/errors.go`
- `internal/adapters/vector/milvus/adapter.go`
- `internal/adapters/vector/stub/adapter.go`
- `internal/adapters/gateway/adapter.go`
- `internal/adapters/gateway/portkey/client.go`
- `internal/adapters/gateway/portkey/reranker.go`
- `internal/runtime/vector_config.go`
- `internal/runtime/gateway_config.go`
- `internal/runtime/providers.go`

Likely modified files:

- `internal/contracts/contracts.go` (additive error taxonomy and optional gateway trace types)
- `internal/runtime/wiring.go` (provider selection wiring)
- `internal/services/retrieval/service.go` (minimal changes only if extra trace fields are added)
- `test/contracts/contract_test.go`
- `test/retrieval/*`
- `test/ingestion/*`

No required changes expected for:

- `ingestion-dashboard-ui/*`
- `search-query-ui/*`

## 10. Cost and Performance Considerations

## 10.1 Cost Model Levers

- Keep Milvus as primary provider to avoid dual-provider production cost in v1.
- Add embedding/rerank response caching (keyed by tenant + request/query hash + model version) with bounded TTL.
- Route fallback mode by tenant traffic class (premium vs standard) to control gateway spend.
- Control observability cardinality:
  - include tenant id and provider labels,
  - avoid per-document high-cardinality labels in Prometheus.

## 10.2 Performance Guardrails

- Adapter overhead budget target: <= 5% added p95 over current retrieval baseline.
- Reuse HTTP connections for gateway calls; set strict timeout budget.
- Enforce top-K limits before rerank to cap latency and token usage.
- Ensure adapter implementations are stateless and horizontally safe.

## 10.3 Right-Sizing by Phase

- v1: single active vector provider (Milvus), single gateway provider (Portkey), stub adapters for tests only.
- v1.1+: optional multi-provider active-active or canary routing if operational data justifies complexity.

## 11. Requirement-to-Design Traceability

| Requirement ID | Design Mapping | Sections |
| --- | --- | --- |
| FR-VDB-001 | Provider-agnostic adapter layer while preserving existing contracts | 2, 3 |
| FR-VDB-002 | Milvus as default runtime provider | 2, 4 |
| FR-VDB-003 | Runtime config-driven provider selection | 4 |
| FR-VDB-004 | Tenant-scoped vector operations | 5, 6 |
| FR-VDB-005 | Normalized provider error taxonomy | 3, 5 |
| FR-VDB-006 | Preserve deterministic retrieval behavior under provider swap | 5, 7, 8 |
| FR-VDB-007 | Fail-fast startup on invalid provider config | 4, 5 |
| FR-PK-001 | AI gateway adapter boundary for retrieval/generation | 2, 3 |
| FR-PK-002 | Portkey as v1 gateway provider | 2, 4 |
| FR-PK-003 | Tenant/request metadata propagation on gateway calls | 3, 6, 7 |
| FR-PK-004 | Timeout/retry/circuit controls for gateway calls | 4, 5, 7 |
| FR-PK-005 | Deterministic fallback behavior with explicit reason | 5, 7, 8 |
| FR-PK-006 | Observability fields: latency/model/token/status/fallback | 3, 10 |
| FR-PK-007 | No frontend contract changes required | 9 |
| NFR-001, NFR-002 | p95/throughput guardrails and adapter budget | 10 |
| NFR-003, NFR-004 | Availability + stateless horizontal-safe adapters | 5, 10 |
| NFR-005, NFR-006 | Deterministic tests and structured observability | 7, 10 |
| MT-001, MT-002, MT-003, MT-004 | Tenant identity enforcement + safe telemetry | 6 |
| GOV-001, GOV-002, GOV-003, GOV-004 | Governance continuity, audit rationale, secret hygiene, security gate | 6, 7 |
| SEC-001..SEC-005 | Secret source/redaction/TLS/error mapping/rotation | 4, 6 |
| DATA-001..DATA-004 | Backward-compatible contracts and lifecycle continuity | 3, 8 |
| INT-001..INT-004 | Runtime wiring and integration failure-path tests | 4, 7 |
| DEP-001..DEP-004 | Implemented in existing runtime/module boundaries | 2, 4, 8 |
| MIG-001..MIG-005 | Source compatibility, parity, migration and rollback | 3, 8 |
| AC-VDB-001..AC-VDB-005 | Stream A acceptance checks | 7, 8 |
| AC-PK-001..AC-PK-005 | Stream B acceptance checks | 7, 8 |
| AC-X-001..AC-X-003 | Cross-stream quality/security checks | 7 |

## 12. Open Risks and Unresolved Decisions

- OQ-001: Portkey scope finalization (generation-only vs retrieval+generation vs all AI calls) is still open.
- OQ-002: Fallback policy choice is still open; add config support for all options and lock default after architecture/security decision.
- OQ-003: First non-Milvus production provider is still open; stub-only portability validation is acceptable in this phase.
- OQ-004: Secret rotation interval enforcement policy remains open; implementation should support rotation regardless of final interval.

Risk watch list:

- Adapter abstraction could regress latency if query translation is inefficient.
- Incomplete error mapping could leak provider-specific semantics into services.
- Missing metadata propagation could break audit/cost attribution.
