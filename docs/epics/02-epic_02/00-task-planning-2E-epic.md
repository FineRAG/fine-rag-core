# Epic E2 Task Planning

Epic: `E2 Governed Async Ingestion`
Date: 2026-03-08

## Epic Objective

Deliver governance-controlled ingestion with profiling, policy decisions, async queue processing, tenant-scoped storage/indexing, and lifecycle retention controls.

## Task Checklist

- [ ] `E2-T1` Ingestion Profiling and Metadata Baseline (`01-2E-Task.md`)
- [ ] `E2-T2` Governance Gatekeeper and Policy Outcomes (`02-2E-Task.md`)
- [ ] `E2-T3` Async Queue Worker and Indexing Path (`03-2E-Task.md`)

## Status Rollup

- Not started: 3
- In progress: 0
- Completed: 0

## Dependency and Sequence

1. Hard prerequisite from E1: `E1-T3` must be complete.
2. Mandatory sequence: `E2-T1 -> E2-T2 -> E2-T3`.
3. Parallelization:
   `E2-T1` may run in parallel with early `E3-T1` planning work, but E3 retrieval validation cannot complete before `E2-T3`.

## Exit Criteria for Epic Completion

1. Ingestion artifacts transition through governed states: `approved`, `quarantine`, `rejected`.
2. Approved content reaches tenant-scoped Milvus index through queue workers with idempotent behavior.
3. Retry, backoff, and DLQ paths are test-verified.
4. Lifecycle retention policies are defined for raw blobs, chunks, and audit records.

## Evidence Rules

1. Task completion requires command output summary and artifact references in each task file.
2. State changes in this file must match evidence in task execution tracking.
