# Distilled Requirements: Vector DB Portability and Portkey Integration

Date: 2026-03-09
Owner: AnalyzerAgent
Status: Draft for clarification and approval
Related sources: `internal/contracts/contracts.go`, `internal/services/retrieval/service.go`, `docs/system_design.md`, `.ai/reviews/requirements.review.md`

## Executive Summary
- This document defines testable requirements for two tracks:
1. Vector DB portability with Milvus as the current provider and a pluggable adapter model.
2. Portkey integration requirements for AI gateway usage in retrieval and generation paths.
- Current repository constraints are explicitly incorporated:
1. Core contracts already exist for `VectorSearcher`, `VectorIndex`, `EmbeddingProvider`, `Reranker`.
2. Retrieval logic currently depends on `contracts.VectorSearcher` and `contracts.Reranker`.
3. No runnable backend API server binary exists in repository root.
4. Frontend apps exist and call API endpoints, but backend endpoint implementation is not present in this repository.

## Final Decision Table (Current Round)
| Decision ID | Topic | Decision | Source/Status |
| --- | --- | --- | --- |
| D-001 | Current VDB provider | Milvus remains current/primary provider for v1 runtime. | Confirmed by user request |
| D-002 | Portability approach | Portability must be through adapter interfaces, not service-level branching logic. | Confirmed by user request + existing contracts |
| D-003 | Existing retrieval dependencies | Retrieval service must continue to depend on `VectorSearcher` and `Reranker` contracts. | Confirmed from `internal/services/retrieval/service.go` |
| D-004 | API surface in this repo | Requirements must not assume immediate backend API implementation in this repository. | Confirmed by user context |
| D-005 | Portkey scope | Portkey required for retrieval/generation path usage; ingestion/embedding path remains unresolved. | Partially resolved; see Open Questions |

## In Scope
- Track A: Vector DB portability requirements for ingestion indexing and retrieval search.
- Track B: Portkey integration requirements for retrieval and generation gateway usage.
- Contract compatibility and migration requirements aligned to existing interfaces.
- Testable acceptance criteria at module, integration, and operational levels.
- Security and secret handling requirements for external adapters and AI gateway calls.

## Out of Scope
- Building a new backend HTTP server binary in this requirement cycle.
- Replacing frontend applications or redefining frontend UX behavior.
- Defining final architecture for unrelated modules (queue, auth, UI deployment).
- Selecting and implementing every alternate Vector DB provider in this cycle.

## Functional Requirements

### Track A: Vector DB Portability
- FR-VDB-001: The platform shall expose a provider-agnostic vector adapter boundary that supports both indexing and search operations through existing contracts (`VectorIndex`, `VectorSearcher`) without changing retrieval service method signatures.
- FR-VDB-002: Milvus shall be implemented and maintained as the default provider behind the adapter boundary.
- FR-VDB-003: Provider selection shall be runtime-configurable (environment/config driven) and shall not require changes to retrieval business logic.
- FR-VDB-004: The adapter boundary shall enforce tenant-scoped operations, requiring tenant context in every search and index call.
- FR-VDB-005: Adapter implementations shall return normalized errors using a stable internal error taxonomy (`validation`, `unavailable`, `timeout`, `unauthorized`, `internal`) to avoid provider-specific leakage in service logic.
- FR-VDB-006: Adapter behavior shall preserve deterministic retrieval expectations already validated in tests (tenant filtering, rerank fallback behavior remains unaffected by provider swap).
- FR-VDB-007: If provider initialization fails at startup, system wiring shall fail fast with explicit diagnostics and no silent fallback to undefined providers.

### Track B: Portkey Integration (Retrieval/Generation)
- FR-PK-001: An AI gateway adapter shall be introduced for retrieval/generation calls so that model-provider details are isolated from retrieval/generation orchestration logic.
- FR-PK-002: Portkey shall be the current AI gateway provider used by this adapter in v1.
- FR-PK-003: All outbound retrieval/generation model calls through the gateway shall include tenant-aware metadata (tenant id, request id) for traceability and cost attribution.
- FR-PK-004: Gateway integration shall support timeout, retry, and circuit-breaker controls to preserve existing reliability behavior for degraded upstream providers.
- FR-PK-005: Gateway failures shall trigger deterministic fallback behavior with explicit trace reason fields and without violating tenant isolation.
- FR-PK-006: Request/response observability fields for retrieval/generation gateway calls shall include latency, model identifier, token usage fields when available, provider status, and fallback reason.
- FR-PK-007: Portkey integration shall not require changes to existing frontend contracts in this repository, because backend API implementation is currently out of repo scope.

## Non-Functional Requirements
- NFR-001 (Latency): Requirements shall preserve baseline retrieval objective alignment: `p95 <= 800 ms` under the platform target load envelope, including adapter overhead budgets.
- NFR-002 (Throughput): Requirements shall remain compatible with platform throughput targets (`300 RPS` steady, `600 RPS` burst) and must not introduce single-threaded bottlenecks in adapter layers.
- NFR-003 (Availability): Adapter and gateway failure handling shall support overall service availability objective alignment (`99.9%` monthly).
- NFR-004 (Scalability): Adapter implementations shall be horizontally safe (stateless or externally coordinated) for multi-instance deployment.
- NFR-005 (Determinism): Existing deterministic service tests for retrieval and governance shall remain green after introducing adapter abstractions.
- NFR-006 (Operability): Adapter selection and gateway behavior shall be observable via structured logs and metrics with tenant-safe labels.

## Multi-Tenancy and Isolation Guarantees
- MT-001: All vector search/index operations shall require tenant identity and reject missing/invalid tenant context.
- MT-002: Search results returned by any provider adapter shall be tenant-filtered before leaving service boundary.
- MT-003: Portkey gateway metadata shall include tenant and request identity for every call; omission is treated as a contract violation.
- MT-004: No adapter log/metric output shall expose cross-tenant payload contents.

## Governance and Compliance Requirements
- GOV-001: Data residency policy checks currently enforced in governance flow shall remain enforceable after portability changes.
- GOV-002: Audit events for retrieval/generation gateway interactions shall include decision/fallback rationale and request identifiers.
- GOV-003: Secrets shall never be hardcoded in code, tests, docs, or configuration checked into source control.
- GOV-004: Security review gate scripts and relevant test suites shall continue passing for modified streams before merge.

## Security and Secrets Handling Requirements
- SEC-001: Portkey API credentials and Vector DB credentials shall be sourced from runtime secret providers or environment injection, never committed in plaintext.
- SEC-002: Secret values shall be redacted in logs and error messages.
- SEC-003: All gateway and vector-provider network calls shall require TLS in transit.
- SEC-004: Authentication/authorization failures from external providers shall be mapped to non-sensitive internal error categories.
- SEC-005: Credential rotation support shall be possible without code changes to service business logic.

## Data Model and Lifecycle States
- DATA-001: Existing `contracts.VectorRecord` fields and validation rules shall remain backward compatible.
- DATA-002: Existing retrieval request/result contracts shall remain backward compatible (`RetrievalQuery`, `RetrievalResult`, `RetrievalTrace`).
- DATA-003: Adapter-level trace fields for provider/gateway interactions shall be additive and non-breaking to existing contract consumers.
- DATA-004: Ingestion lifecycle states (`queued`, `approved`, `quarantine`, `rejected`, `indexing`, `indexed`, `failed`) remain authoritative and unaffected by provider portability refactor.

## Integration Requirements
- INT-001: Runtime wiring shall support configuring Milvus adapter as active vector provider without code edits.
- INT-002: Runtime wiring shall support configuring Portkey adapter for retrieval/generation gateway without code edits.
- INT-003: Retrieval service integration shall continue consuming `VectorSearcher` and `Reranker` abstractions without direct dependency on concrete provider SDKs.
- INT-004: Integration tests shall cover provider adapter failures, timeout handling, and fallback observability paths.

## Deployment and Runtime Constraints
- DEP-001: Requirements must be implementable within existing Go module layout and runtime wiring model in `internal/runtime`.
- DEP-002: Because no backend API server binary exists in repo root, validation scope for this stream is module/integration test level plus wiring/bootstrap checks.
- DEP-003: Runtime configuration must explicitly fail on missing required provider/gateway config.
- DEP-004: Default deployment region constraints from platform requirements remain applicable.

## Migration and Backward Compatibility Requirements
- MIG-001: Existing service constructors and interfaces used by tests shall remain source-compatible or include transitional wrappers.
- MIG-002: Milvus behavior must remain parity-baseline during and after adapter extraction.
- MIG-003: No required frontend API contract changes may be introduced from this stream alone.
- MIG-004: Existing tests under `test/retrieval`, `test/ingestion`, and `test/governance` shall pass after refactor.
- MIG-005: If new adapter interfaces are introduced, migration notes must map old-to-new wiring and rollback path.

## Test and Acceptance Criteria (Verifiable Checks)

### Stream A Acceptance (Vector DB Portability)
- AC-VDB-001: `go test ./... -run 'Retrieval|TenantFilter' -count=1` passes with Milvus adapter selected.
- AC-VDB-002: Contract tests verify retrieval service compiles and runs unchanged against at least one non-Milvus stub adapter implementing `VectorSearcher`/`VectorIndex`.
- AC-VDB-003: Provider switch via configuration changes active adapter without changing retrieval service code files.
- AC-VDB-004: Negative test verifies startup fails with explicit error when configured provider is unknown/misconfigured.
- AC-VDB-005: Tenant isolation tests prove no cross-tenant documents are returned after adapter abstraction.

### Stream B Acceptance (Portkey Integration)
- AC-PK-001: Gateway adapter unit tests verify request metadata propagation (`tenant_id`, `request_id`) on outbound calls.
- AC-PK-002: Timeout and retry tests verify deterministic fallback and populated fallback reason in retrieval trace.
- AC-PK-003: Secrets handling tests verify credentials are loaded from environment/runtime config and never echoed in logs.
- AC-PK-004: Observability tests verify latency/status/token fields are emitted when available.
- AC-PK-005: Integration tests verify retrieval/generation path can execute through Portkey adapter with mocked gateway responses.

### Cross-Stream Quality Gates
- AC-X-001: `scripts/securitygov_review.sh` for impacted streams passes.
- AC-X-002: Existing deterministic retrieval/gov tests continue passing after integration.
- AC-X-003: No raw secrets appear in repository grep checks for provider/gateway credentials.

## Dependencies and Assumptions
- DEPEND-001 (Dependency): Access to Milvus runtime and credentials for integration validation.
- DEPEND-002 (Dependency): Access to Portkey project/workspace credentials for gateway integration validation.
- DEPEND-003 (Dependency): Existing contract package remains stable during this requirement stream.
- ASM-001 (Assumption): Retrieval/generation orchestration code will be implemented in backend modules before frontend endpoint integration is validated end-to-end.
- ASM-002 (Assumption): Non-Milvus providers can initially be validated through contract-compliant stubs/mocks if production provider access is unavailable.
- ASM-003 (Assumption): Existing SLO targets remain the acceptance baseline unless superseded by product/security approval.

## Risks
- RISK-001: Adapter abstraction may regress performance if provider-specific optimizations are lost.
- RISK-002: Incomplete error normalization may leak provider-specific failure behavior into service logic.
- RISK-003: Missing gateway metadata propagation may break tenant-level audit/cost attribution.
- RISK-004: Secret misconfiguration risk increases with additional external provider integrations.

## Open Questions (Owner + Decision Deadline)
- OQ-001 (Owner: Product + Architecture, Deadline: 2026-03-12): Should Portkey be mandatory only for generation, or for both retrieval-time model calls and generation calls?
1. Option A: Mandatory for generation only.
2. Option B: Mandatory for retrieval-time model calls and generation.
3. Option C: Mandatory for all AI calls including embedding.

- OQ-002 (Owner: Architecture, Deadline: 2026-03-12): What is the required fallback policy when Portkey is unavailable?
1. Option A: Fail closed (error response, no alternate provider).
2. Option B: Fail over to direct provider with strict allowlist.
3. Option C: Return retrieval-only response without generation.

- OQ-003 (Owner: Platform, Deadline: 2026-03-12): Which alternate Vector DB provider must be validated first beyond Milvus?
1. Option A: pgvector.
2. Option B: OpenSearch vector.
3. Option C: Weaviate.
4. Option D: Stub-only in this phase, production provider in next phase.

- OQ-004 (Owner: Security, Deadline: 2026-03-12): What is the required secret rotation interval and enforcement mechanism for Portkey and VDB credentials?
1. Option A: 30 days.
2. Option B: 60 days.
3. Option C: 90 days.
4. Option D: Provider-managed rotation policy accepted.

## Decision Log Appendix
| Date | Item | Decision/Update | Status |
| --- | --- | --- | --- |
| 2026-03-09 | Track split | Requirements split into Track A (VDB portability) and Track B (Portkey). | Confirmed |
| 2026-03-09 | Current provider baseline | Milvus retained as current provider; portability via adapter contracts. | Confirmed |
| 2026-03-09 | Portkey scope | Retrieval/generation scope noted from request, detailed scope still needs final option selection. | Open |
| 2026-03-09 | Validation scope | Module/integration verification emphasized due to missing backend server binary in repo. | Confirmed |
