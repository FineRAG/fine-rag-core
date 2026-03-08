# Go-RAG Epic Summary (Latest)

Date: 2026-03-08  
Stage: Epic Planning  
Source Inputs: `docs/system_design.md`, `docs/distilled_requirements.md`  
Gate State: `Proceed to Task Packs = unchecked` in `.ai/reviews/epic.review.md`

## 1) Executive Summary

This plan decomposes the approved Go-RAG architecture into three medium-sized epics that preserve all stated v1 constraints: multi-tenant isolation, governed async ingestion, low-latency retrieval, security/compliance controls, and EC2-based deployment in `ap-south-1`.

Because the epic review gate is currently pending, this run publishes the refreshed epic summary and dependency map only. Task-pack artifacts are intentionally withheld until the gate is checked, per workflow policy.

## 2) Epic Dependency Graph (Sequential vs Parallel)

Execution model target: maximum 2 active epics in parallel.

1. `E1 Foundation and Tenant Guardrails` (must start first)
2. `E2 Governed Async Ingestion` (starts after E1 core interfaces and tenant context are stable)
3. `E3 Retrieval, SLO Hardening, and Operability` (can start in parallel with late E2 once E1 contracts are fixed)

Hard dependency edges:

1. `E1 -> E2` for tenant context propagation, auth, repository contracts, and rate-limit policy.
2. `E1 -> E3` for API contracts, tenant-aware data access abstractions, and observability baseline.
3. `E2 -> E3` for indexed corpus availability, governance-state-aware retrieval filters, and end-to-end quality validation.

Parallelization guidance:

1. Allowed parallel window A: late `E2` indexing/worker work alongside early `E3` retrieval API scaffolding.
2. Allowed parallel window B: E2 lifecycle retention jobs alongside E3 dashboard/alert wiring.
3. Disallowed: any retrieval relevance or SSE completion tests before E2 indexing path reaches testable readiness.

## 3) Detailed Epic Definitions

### Epic `E1`: Foundation and Tenant Guardrails

Objective:
Establish service skeleton, API contracts, tenant isolation enforcement, auth, and quota/rate controls required by all downstream streams.

Requirements and design sections covered:

1. Requirements: sections `2`, `3`, `5`, `8` in `docs/distilled_requirements.md`.
2. Design: sections `2`, `4`, `6`, `9`, `11` in `docs/system_design.md`.

In scope:

1. Core module boundaries and service interfaces.
2. Tenant context middleware and fail-fast enforcement.
3. API-key auth (hashed/salted, revoke-only lifecycle).
4. Global/tenant rate limits and quota defaults (`2 RPS` steady, `4 RPS` burst; global burst cap `600 RPS`).
5. Baseline observability hooks for request/latency/error tracing.

Out of scope:

1. Temporal orchestration.
2. Full retrieval ranking quality tuning.
3. Advanced production autoscaling policy automation.

Dependencies:

1. Hard: none (entry epic).
2. Soft: final queue provider lock confirmation and reranker vendor contract for downstream epics.

Risks and rollback notes:

1. Risk: inconsistent tenant propagation may cause leakage.
2. Rollback: disable affected endpoint routes and fall back to strict middleware reject until isolation tests pass.

Entry criteria:

1. Approved design and requirements available.
2. Review gate allows epic planning updates.

Exit criteria:

1. All API flows reject missing/invalid tenant context.
2. Auth and rate limit controls are test-covered and measurable against policy defaults.
3. Integration checks prove no cross-tenant read/write leakage in foundational paths.

### Epic `E2`: Governed Async Ingestion

Objective:
Deliver governed ingestion path with profiler/policy states, blob persistence, queue-based async processing, embedding/index writes, idempotency, and lifecycle controls.

Requirements and design sections covered:

1. Requirements: sections `2`, `3`, `4`, `7`, `8`, `10` in `docs/distilled_requirements.md`.
2. Design: sections `2`, `3`, `5`, `7`, `8`, `10`, `11` in `docs/system_design.md`.

In scope:

1. Ingestion job API and orchestrator state transitions (`approved`, `quarantine`, `rejected`).
2. MinIO tenant-scoped object pathing and metadata persistence.
3. Queue producer/consumer path (v1 queue-first, no Temporal).
4. Worker chunking + embedding adapter route + Milvus writes with mandatory tenant filter field.
5. Retry/backoff, idempotency keys, DLQ handling.
6. Retention/lifecycle enforcement for raw blobs/chunks/audit classes.

Out of scope:

1. Full human approval UI/workflow.
2. Alternative queue migrations (Redis streams) beyond documented decision records.

Dependencies:

1. Hard: E1 tenant/auth/rate-limit foundations.
2. Soft: AI provider throughput profile and final queue platform lock confirmation.

Risks and rollback notes:

1. Risk: queue backlog growth under embedding slowdown.
2. Rollback: throttle or defer ingestion submission, activate DLQ replay controls, and apply conservative worker concurrency profile.

Entry criteria:

1. E1 contracts and middleware are stable.
2. Storage credentials and tenant metadata schema contracts are available.

Exit criteria:

1. Ingestion jobs deterministically transition through governed states with audit trails.
2. Approved artifacts index successfully with tenant-scoped Milvus metadata.
3. Retry and DLQ behavior is verifiable under induced failures.

### Epic `E3`: Retrieval, SLO Hardening, and Operability

Objective:
Implement tenant-safe retrieval with rerank/SSE completion and operational controls to meet latency/availability/cost observability targets.

Requirements and design sections covered:

1. Requirements: sections `1`, `3`, `5`, `6`, `8`, `9` in `docs/distilled_requirements.md`.
2. Design: sections `2`, `3`, `6`, `7`, `8`, `9`, `11` in `docs/system_design.md`.

In scope:

1. Search API flow with tenant-bound retrieval from Milvus.
2. Managed reranker integration and timeout fallback.
3. SSE streaming response contract with citations and trace metadata.
4. RED + TTFT + token-cost observability and alert baselines.
5. Performance and reliability gates for `300 RPS` steady and `p95 <= 800 ms`.

Out of scope:

1. gRPC transport enablement.
2. Temporal governance-service split.

Dependencies:

1. Hard: E1 interfaces and tenant enforcement.
2. Hard: E2 index availability for realistic retrieval tests.
3. Soft: managed reranker procurement finalization.

Risks and rollback notes:

1. Risk: reranker latency breaches p95 budget.
2. Rollback: enforce rerank timeout/circuit-breaker and degrade gracefully to non-reranked ordering with audit flag.

Entry criteria:

1. E1 complete and E2 indexing path minimally available.
2. Observability stack endpoints prepared for metric ingestion.

Exit criteria:

1. Retrieval path meets latency and error-budget guardrails in load tests.
2. Tenant isolation is preserved across query, rerank, and citation output.
3. Dashboards and alarms expose token usage, TTFT, and RED with actionable thresholds.

## 4) Epic-to-Phase Mapping

1. Phase 1 (Milestone v1 tasks 1-4 in design): primarily `E1` and `E2` core delivery.
2. Phase 2 (v1 hardening): primarily `E3` plus late E2 operational stabilization.
3. Phase 3 (v1.1): out of current epic scope (Temporal and governance-service split).

## 5) Open Decisions and Clarifications Required

1. Epic review gate action: confirm when to check `Proceed to Task Packs` in `.ai/reviews/epic.review.md` so detailed task artifacts can be generated.
2. Queue platform lock: confirm v1 production lock remains `SQS` with Redis deferred to v1.1 evaluation.
3. Reranker vendor choice: finalize SLA and pricing baseline required for E3 performance-risk closure.
4. API-key governance detail: confirm max active keys per tenant and whether dual-control revoke is required for regulated tenants.

## 6) Next Artifact Policy

Per review-gate policy, the following are generated only after `Proceed to Task Packs` is checked:

1. `docs/epics/00-task-planning-across-all-epics.md`
2. Per-epic task packs under `docs/epics/<epic-folder>/`

