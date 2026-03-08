# Task Planning Across All Epics

Date: 2026-03-08
Stage: Task Packs (Planning Only)
Inputs: `docs/distilled_requirements.md`, `docs/system_design.md`, `docs/epics/00-epic_summary.md`, `.ai/reviews/epic.review.md`, `.ai/reviews/tasks.review.md`

## Cross-Epic Dependency Map

1. `E1-T1 -> E1-T2 -> E1-T3`
2. `E1-T3 -> E2-T1`
3. `E2-T1 -> E2-T2 -> E2-T3`
4. `E1-T3 -> E3-T1`
5. `E2-T3 -> E3-T1`
6. `E3-T1 -> E3-T2 -> E3-T3`

## Parallelization Rules

1. Allowed in parallel after `E1-T3` complete:
   `E2-T1` and `E3-T1` planning/design readiness work may start together.
2. Not allowed in parallel:
   `E3-T1` acceptance validation cannot be marked complete until `E2-T3` provides indexed corpus readiness evidence.
3. Allowed in parallel after `E2-T3` complete:
   `E3-T2` compliance validation and `E3` performance/load verification prep can run together.

## Global Task Board

- [x] `E1-T1` Core Contracts and Service Skeleton (`docs/epics/01-epic_01/01-1E-Task.md`)
- [x] `E1-T2` Tenant Context Middleware and Isolation Guards (`docs/epics/01-epic_01/02-1E-Task.md`)
- [ ] `E1-T3` API Key Auth and Rate-Limit Enforcement (`docs/epics/01-epic_01/03-1E-Task.md`)
- [ ] `E2-T1` Ingestion Profiling and Metadata Baseline (`docs/epics/02-epic_02/01-2E-Task.md`)
- [ ] `E2-T2` Governance Gatekeeper and Policy Outcomes (`docs/epics/02-epic_02/02-2E-Task.md`)
- [ ] `E2-T3` Async Queue Worker and Indexing Path (`docs/epics/02-epic_02/03-2E-Task.md`)
- [ ] `E3-T1` Retrieval API, Milvus Tenant Filtering, and Rerank Integration (`docs/epics/03-epic_03/01-3E-Task.md`)
- [ ] `E3-T2` SLO, Security, and Governance Validation (`docs/epics/03-epic_03/02-3E-Task.md`)
- [ ] `E3-T3` Operability, Release Readiness, and Stage Handoff (`docs/epics/03-epic_03/03-3E-Task.md`)

## Status Rollup

- Not started: 7
- In progress: 0
- Completed: 2

## Evidence Policy

1. Checkbox updates require explicit evidence in the task file execution tracking section.
2. Accepted evidence examples:
   test command output summary, review artifact links, metric screenshots, or signed-off gate checklist updates.
3. Until evidence exists, all tasks remain `- [ ]` by policy.

## Gate Alignment

1. Epic review gate: checked (`Proceed to Task Packs`).
2. Task pack review gate: checked (`Proceed to Coding/Testing/Security/Deployment Execution`).
3. Execution is authorized for approved tasks; dependency order remains mandatory.
