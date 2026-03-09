# Epic E7 Task Planning

Epic: `E7 Ingestion Dashboard Backend Auth + MinIO Upload + Progress`
Date: 2026-03-09
Task Pack State: created for execution

## Task Checklist

- [x] `E7-T1` Login page redesign to username/password and backend token auth
- [x] `E7-T2` Tenant switcher and dashboard KB/vector metrics sections
- [x] `E7-T3` Presigned MinIO upload flow with folder allowlist filter
- [x] `E7-T4` Ingestion progress SSE + per-file counters/governance + retry failed
- [x] `E7-T5` PWA compatibility (manifest/service worker) and regression validation

## Acceptance Criteria Summary

1. Login form has only username and password.
2. Dashboard loads on auth success and exposes tenant + KB/vector metrics.
3. Upload uses presigned URLs and submits ingestion jobs by object references.
4. User sees ongoing ingestion status with stage-level and per-file counters.
5. UI is installable/PWA-compatible and tests/build pass.

## Execution Tracking

- Not started: 0
- Completed: 5

## Evidence

1. `npm --prefix ingestion-dashboard-ui test` -> PASS (8 tests).
2. `npm --prefix ingestion-dashboard-ui run build` -> PASS.
