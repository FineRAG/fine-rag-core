# Distilled Requirements: Ingestion Dashboard Backend Auth + S3 Upload

Date: 2026-03-09
Status: Approved by user answers
Scope: `ingestion-dashboard-ui`

## Finalized Decisions

1. Login form fields: `username` + `password` only.
2. Auth source: fully dynamic backend auth (no hardcoded demo credentials).
3. Tenant resolution: auth/tenant API provides memberships; auto-open when one tenant exists.
4. Request ID: internal only, auto-generated and sent in headers; not user input.
5. Dashboard content: aggregate cards + list/table for knowledge base.
6. Vector size metric: show both vector count and storage size.
7. Tenant switching: tenant switcher in header.
8. Upload path: presigned URL direct upload to S3.
9. Folder upload behavior: recursive with extension allowlist.
10. Pipeline configuration: fixed in v1 (cleanup/classification/other stages).
11. Progress granularity: stage-level + per-file counters.
12. Live updates transport: SSE.
13. Auth artifact: session/JWT token from auth API.
14. Governance visibility: decision + policy code + reason.
15. Partial failures: per-file success/failure + retry failed.
16. Session expiry handling: prompt re-login and resume tracking.
17. Platform: UI must be PWA compatible.

## Functional Requirements

1. Unauthenticated users only see a login card with `username` and `password`.
2. On login success, UI fetches tenants and resolves active tenant context.
3. Dashboard shows:
- active tenant id and tenant switch control,
- knowledge-base aggregate metrics and list,
- vector aggregate metrics (count + storage),
- upload controls for files/folders,
- ingestion progress feed and job table.
4. Local uploads use presigned upload contract before ingestion submit.
5. Ingestion pipeline details are visible in UI status surfaces, including governance fields.
6. Retry action is available for failed file records.
7. UI handles auth expiry deterministically and preserves resumable tracking context.

## API Contract Expectations

1. `POST /api/v1/auth/login` body: `{ username, password }` -> `{ token }`.
2. `GET /api/v1/tenants` -> tenant memberships.
3. `GET /api/v1/knowledge-bases` -> list and aggregate-compatible fields.
4. `GET /api/v1/tenants/{tenantId}/vector-stats` -> `{ vectorCount, storageBytes }`.
5. `POST /api/v1/uploads/presign` -> upload URLs/object keys for selected files.
6. `POST /api/v1/ingestion/jobs` -> enqueue ingest by URI/object references.
7. `GET /api/v1/ingestion/jobs` -> current job states.
8. `GET /api/v1/ingestion/jobs/stream` (SSE) -> stage and per-file progress updates.
9. Auth-protected routes require `Authorization`, `X-Tenant-ID`, `X-Request-ID`.

## Non-Functional Requirements

1. No raw password in logs, UI debug surfaces, or persistent storage.
2. Folder selection is filtered by allowlist to prevent unsupported types.
3. PWA compatibility includes manifest + service worker registration.
4. Degraded API behavior is surfaced with explicit user status messages.
