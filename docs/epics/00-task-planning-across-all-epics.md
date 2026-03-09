# Task Planning Across All Epics

Date: 2026-03-09
Stage: Task Packs (Planning Only)
Inputs: `docs/distilled_requirements.md`, `docs/system_design.md`, `docs/distilled_requirements_vdb_portkey.md`, `docs/system_design_vdb_portkey.md`, `docs/epics/00-epic_summary.md`, `.ai/reviews/epic.review.md`, `.ai/reviews/tasks.review.md`
Task Pack State: approved for downstream agents (review gates are checked)

## Cross-Epic Dependency Map

1. `E1-T1 -> E1-T2 -> E1-T3`
2. `E1-T3 -> E2-T1`
3. `E2-T1 -> E2-T2 -> E2-T3`
4. `E1-T3 -> E3-T1`
5. `E2-T3 -> E3-T1`
6. `E3-T1 -> E3-T2 -> E3-T3`
7. `E3-T3 -> E4-T1`
8. `E3-T3 -> E4-T2`
9. `E3-T3 -> E4-T3 -> E4-T4`
10. `E2-T3 -> E4-T5`
11. `E4-T3 -> E4-T5` for persistent queue state and audit durability wiring
12. `E4-T5 -> E5-T1`
13. `E5-T1 -> E5-T2`
14. `E5-T1 -> E5-T3`
15. `E5-T1 -> E5-T4`
16. `E5-T2 -> E5-T5`
17. `E5-T4 -> E5-T5`
18. `E5-T3 -> E5-T6`
19. `E5-T5 -> E5-T6`

## Parallelization Rules

1. Maximum active epics in parallel: `2`.
2. Post-E3 window (allowed): `E4-T1` and `E4-T3` can run together.
3. Post-E3 window (allowed): `E4-T2` and `E4-T5` can run together after shared API contract confirmation.
4. Not allowed: `E4-T4` cannot start before `E4-T3` repository interfaces and persistence adapters are merged.
5. Not allowed: final E4 closeout cannot complete until both frontend tracks (`E4-T1`, `E4-T2`) and infra tracks (`E4-T4`, `E4-T5`) are completed.
6. E5 Window A (allowed): `E5-T1` and `E5-T4` can run in parallel once E4 closeout is confirmed.
7. E5 Window B (allowed): `E5-T2` and `E5-T3` can run in parallel after `E5-T1` merges.
8. Not allowed: `E5-T5` cannot start until both `E5-T2` and `E5-T4` are complete.
9. Not allowed: `E5-T6` is final closeout only after `E5-T3` and `E5-T5` are complete.

## Global Task Board

- [x] `E1-T1` Core Contracts and Service Skeleton (`docs/epics/01-epic_01/01-1E-Task.md`)
- [x] `E1-T2` Tenant Context Middleware and Isolation Guards (`docs/epics/01-epic_01/02-1E-Task.md`)
- [x] `E1-T3` API Key Auth and Rate-Limit Enforcement (`docs/epics/01-epic_01/03-1E-Task.md`)
- [x] `E2-T1` Ingestion Profiling and Metadata Baseline (`docs/epics/02-epic_02/01-2E-Task.md`)
- [x] `E2-T2` Governance Gatekeeper and Policy Outcomes (`docs/epics/02-epic_02/02-2E-Task.md`)
- [x] `E2-T3` Async Queue Worker and Indexing Path (`docs/epics/02-epic_02/03-2E-Task.md`)
- [x] `E3-T1` Retrieval API, Milvus Tenant Filtering, and Rerank Integration (`docs/epics/03-epic_03/01-3E-Task.md`)
- [x] `E3-T2` SLO, Security, and Governance Validation (`docs/epics/03-epic_03/02-3E-Task.md`)
- [x] `E3-T3` Operability, Release Readiness, and Stage Handoff (`docs/epics/03-epic_03/03-3E-Task.md`)
- [x] `E4-T1` Ingestion Dashboard UI Package (React + Vite) (`docs/epics/04-epic_04/01-4E-Task.md`)
- [x] `E4-T2` Search Query UI Package (React + Vite) (`docs/epics/04-epic_04/02-4E-Task.md`)
- [x] `E4-T3` PostgreSQL Repository Adapter and Persistence Wiring (`docs/epics/04-epic_04/03-4E-Task.md`)
- [x] `E4-T4` External PostgreSQL URL Integration and Migration Bootstrap Strategy (`docs/epics/04-epic_04/04-4E-Task.md`)
- [x] `E4-T5` AWS SQS + DLQ Adapter Wiring, Secrets, and Local Fallback (`docs/epics/04-epic_04/05-4E-Task.md`)
- [x] `E5-T1` Provider-Agnostic VDB Adapter Boundary and Wiring Freeze (`docs/epics/05-epic_05/01-5E-Task.md`)
- [x] `E5-T2` Milvus Adapter Hardening and Error Taxonomy Enforcement (`docs/epics/05-epic_05/02-5E-Task.md`)
- [x] `E5-T3` Alternate VDB Adapter Contract Validation Path (Stub/Smoke) (`docs/epics/05-epic_05/03-5E-Task.md`)
- [x] `E5-T4` Portkey Gateway Adapter Integration and Fallback Policy Wiring (`docs/epics/05-epic_05/04-5E-Task.md`)
- [x] `E5-T5` Secrets, Observability, and Governance Controls for VDB/Gateway (`docs/epics/05-epic_05/05-5E-Task.md`)
- [x] `E5-T6` Documentation and Operational Runbook Closure (`docs/epics/05-epic_05/06-5E-Task.md`)

## Status Rollup

- Not started: 0
- In progress: 0
- In security/deploy evidence: 0
- Completed: 20

## Evidence Policy

1. Checkbox updates require explicit evidence in the task file execution tracking section.
2. Accepted evidence examples:
   test command output summary, review artifact links, metric screenshots, or signed-off gate checklist updates.
3. Until evidence exists, all tasks remain `- [ ]` by policy.

## Gate Alignment

1. Epic review gate: checked (`Proceed to Task Packs`).
2. Task pack review gate: checked (`Proceed to Coding/Testing/Security/Deployment Execution`).
3. E5 task packs are approved for CodingAgent, TestingAgent, and SecurityGovAgent execution when dependency prerequisites are met.
