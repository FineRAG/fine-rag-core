# Epic E4 Task Planning

Epic: `E4 Frontend and Production Integrations`
Date: 2026-03-09
Task Pack State: approved for downstream execution (gates checked)

## Epic Objective

Close post-E3 missing v1 scope from `docs/system_design.md` by adding both required frontend packages and production-grade PostgreSQL and SQS integrations.

## Requirements and Design Sections Covered

1. Requirements: `2`, `3`, `5`, `7`, `8`, `9`, `10` in `docs/distilled_requirements.md`.
2. Design: sections `2`, `3`, `4`, `5`, `8`, `9`, `10`, `11`, `12` in `docs/system_design.md`.

## Scope Boundaries

In scope:
1. `ingestion-dashboard-ui` React + Vite package setup and integration path.
2. `search-query-ui` React + Vite package setup and integration path.
3. PostgreSQL repository adapters for tenant registry, metadata, and audit persistence.
4. External DB URL integration and migration/bootstrap strategy.
5. AWS SQS + DLQ producer/consumer integration with secrets/config and local/dev fallback.

Out of scope:
1. HITL approval UI workflow.
2. Temporal migration.
3. Alternative production queue providers.

## Task Checklist

- [x] `E4-T1` Ingestion Dashboard UI Package (React + Vite) (`01-4E-Task.md`)
- [x] `E4-T2` Search Query UI Package (React + Vite) (`02-4E-Task.md`)
- [ ] `E4-T3` PostgreSQL Repository Adapter and Persistence Wiring (`03-4E-Task.md`)
- [ ] `E4-T4` External PostgreSQL URL Integration and Migration Bootstrap Strategy (`04-4E-Task.md`)
- [ ] `E4-T5` AWS SQS + DLQ Adapter Wiring, Secrets, and Local Fallback (`05-4E-Task.md`)

## Entry Criteria

1. `E3-T3` is completed and documented in `docs/epics/00-task-execution-status.md`.
2. Review gates remain checked in `.ai/reviews/epic.review.md` and `.ai/reviews/tasks.review.md`.

## Dependency Map

Hard dependencies:
1. `E3-T3 -> E4-T1`
2. `E3-T3 -> E4-T2`
3. `E3-T3 -> E4-T3 -> E4-T4`
4. `E2-T3 -> E4-T5`
5. `E4-T3 -> E4-T5` for persistent queue state/audit alignment

Soft dependencies:
1. AWS IAM and SQS credentials availability.
2. PostgreSQL hosted instance network allowlist and credentials.

## Parallelization Plan

1. Window A: run `E4-T1` and `E4-T3` in parallel.
2. Window B: run `E4-T2` and `E4-T5` in parallel after API contract confirmation.
3. `E4-T4` is strictly sequential after `E4-T3`.

## Risks and Rollback Notes

1. Risk: frontend package drift from API contract versions.
2. Risk: DB startup failures due to bad external URL or missing migrations.
3. Risk: queue backlog due to SQS/DLQ misconfiguration.
4. Rollback: keep existing in-memory adapters and current deployment path behind explicit feature flags until production integrations pass acceptance checks.

## Exit Criteria

1. Both frontend packages are present and buildable.
2. PostgreSQL adapter wiring is implemented for tenant registry, metadata, and audit persistence.
3. External DB URL runtime path and migration/bootstrap process are documented and test-validated.
4. SQS + DLQ integration is implemented with secrets/config and local/dev fallback.
5. All E4 tasks include evidence in execution tracking before checkboxes are marked complete.

## Evidence Rules

1. Task checkboxes can be marked complete only with command evidence in each task file.
2. If a blocker remains, document it in the task file Notes section with owner and next action.

## Status Rollup

- Not started: 3
- In progress: 0
- Completed: 2
