# System Design: Ingestion Dashboard Backend Auth + S3 Presigned Upload

Date: 2026-03-09
Status: Implementation design
Applies to: `ingestion-dashboard-ui`

## Architecture Summary

1. Replace login bootstrap (`username + apiKey + requestId`) with backend auth login (`username + password`).
2. Use token-based auth session and internal request-id generation for all protected calls.
3. Add tenant switcher and dashboard metrics surfaces for knowledge base + vector storage.
4. Implement S3 presigned upload workflow for local file/folder ingestion.
5. Add ingestion progress stream using SSE and merge event updates into job table.
6. Add PWA compatibility via static manifest and service worker registration.

## UI State Model

1. Auth states: `logged_out`, `authenticating`, `authenticated`, `expired`, `error`.
2. Tenant state: unresolved, selected, switched.
3. Upload state: selecting, presigning, uploading, submitting, queued, failed.
4. Progress state: snapshot (`GET jobs`) + stream (`SSE updates`).

## Data Model Additions

1. `KnowledgeBaseRecord`: id/name/documentCount/chunkCount/lastIngestedAt.
2. `VectorStats`: vectorCount/storageBytes/updatedAt.
3. `IngestionProgressEvent`: jobId/stage/counters/governance fields/per-file status.

## Upload Flow (Presigned S3)

1. User selects files or folder.
2. UI applies extension allowlist.
3. UI requests presigned upload batch from backend.
4. UI PUT uploads each file directly to S3 using returned upload URL.
5. UI submits ingestion job with uploaded object references.
6. Backend triggers fixed pipeline (cleanup/classification/indexing).

## Progress/Monitoring Flow

1. UI loads initial jobs snapshot.
2. UI opens SSE stream endpoint and applies incoming job updates.
3. UI renders stage and per-file counters with governance outcomes.
4. UI allows retry for failed file records.

## Error Strategy

1. Auth 401/403 -> reset to login and show relogin message.
2. Presign/upload errors -> show explicit upload failure status and keep selection for retry.
3. SSE disconnect -> fallback to periodic refresh.

## PWA Compatibility

1. Add `public/manifest.webmanifest`.
2. Add `public/service-worker.js` for basic offline shell caching.
3. Register service worker from `src/main.tsx` in production.

## Task Pack

1. T1 Login/auth refactor (username/password only, backend token).
2. T2 Tenant switch + KB/vector metrics surfaces.
3. T3 Presigned S3 upload and folder allowlist filtering.
4. T4 Ingestion progress stream + per-file counters/governance and retry.
5. T5 PWA assets + service worker registration + tests/build validation.
