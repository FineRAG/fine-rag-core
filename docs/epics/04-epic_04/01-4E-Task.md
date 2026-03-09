# E4-T1 Ingestion Dashboard UI Package (React + Vite)

## Why This Task Exists

`docs/system_design.md` requires an `ingestion-dashboard-ui` frontend package, but the repository currently has no implementation package for tenant upload/status and API-key management flows.

## Inputs and Prerequisites

1. `E3-T3` completed.
2. API contracts for ingestion jobs and API-key management available from current backend modules.
3. Existing deployment topology constraints from `docs/system_design.md` section 5.

## Files and Modules Expected to Change

1. New package folder: `ingestion-dashboard-ui/` (Vite React app).
2. Frontend build/deploy wiring: likely updates to `docker-compose.yml`, `README.md`, and deployment scripts under `scripts/`.
3. Shared API contract docs under `docs/` if endpoint payload assumptions need explicit documentation.

## CodingAgent Instructions

1. Scaffold `ingestion-dashboard-ui` using React + Vite.
2. Implement minimal production-ready screens: tenant login/session bootstrap, file/folder ingestion submission, ingestion job status table, API-key create/revoke controls.
3. Add environment-based API base URL configuration.
4. Ensure responsive layout for desktop and mobile breakpoints.
5. Keep UI package isolated and independently buildable.

## TestingAgent Instructions

1. Add frontend unit tests for request serialization, status rendering, and API-key action handlers.
2. Add at least one integration-style test for upload/job-status flow with mocked backend responses.
3. Validate build and lint checks for the new package.
4. Verify mobile and desktop rendering baseline using deterministic viewport checks.

## SecurityGovAgent Instructions

1. Validate no secrets are hardcoded in frontend source.
2. Verify tenant context is explicitly passed in all API calls requiring tenant scoping.
3. Confirm API-key values are never logged or persisted in browser local storage beyond stated policy.
4. Review CSP and frontend security header assumptions in deployment docs.

## Acceptance Criteria (Measurable)

1. `ingestion-dashboard-ui/` package exists and `npm run build` succeeds.
2. UI includes screens for login/session, ingestion submission, status listing, and API-key create/revoke.
3. Frontend tests pass with no failures.
4. Deployment docs/scripts include a reproducible path to run and serve the UI.

## Suggested Validation Commands

```bash
cd ingestion-dashboard-ui && npm ci && npm run lint && npm run test && npm run build

go test ./... -run Ingestion|APIKey -count=1
```

## Execution Tracking

- [x] Started
- [x] Completed
- Evidence:
  - Coding PASS: `ingestion-dashboard-ui/` created with React + Vite package, tenant session bootstrap, ingestion submission/status table, API-key create/revoke controls, and environment-based API URL (`VITE_INGESTION_API_BASE_URL`).
  - Frontend validation PASS: `npm --prefix ingestion-dashboard-ui run lint`; `npm --prefix ingestion-dashboard-ui run test`; `npm --prefix ingestion-dashboard-ui run build`.
  - Backend compatibility PASS: `go test ./... -run 'Ingestion|APIKey' -count=1`.
  - SecurityGov PASS: `scripts/securitygov_review.sh 'E4-T1|Ingestion|APIKey|Frontend|React|Vite'`.
  - Deployment + health PASS: `scripts/deploy_sync_health.sh` (service `ingestion-dashboard-ui` running and exposed on `:14173`; stack checks PASS).
  - Git flow executed: `scripts/git_task_flow.sh create-branch E4-T1 ingestion-dashboard-ui-package`.
- Notes:
  - API key value is displayed once in-memory and is not persisted to local storage.
