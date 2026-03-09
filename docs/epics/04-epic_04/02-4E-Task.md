# E4-T2 Search Query UI Package (React + Vite)

## Why This Task Exists

`docs/system_design.md` requires a dedicated `search-query-ui` frontend package for tenant query workflows, SSE answer streaming, and citation display. This package is currently missing.

## Inputs and Prerequisites

1. `E3-T3` completed.
2. Retrieval API and SSE response contract from retrieval service are stable enough for UI consumption.
3. Citation and trace metadata contract available from backend interfaces.

## Files and Modules Expected to Change

1. New package folder: `search-query-ui/` (Vite React app).
2. Frontend runtime config wiring in `docker-compose.yml` and deployment scripts under `scripts/`.
3. Documentation updates in `README.md` and task docs for run/build instructions.

## CodingAgent Instructions

1. Scaffold `search-query-ui` using React + Vite.
2. Implement query input UI, streaming answer panel (SSE), citation list panel, request status/error states.
3. Add tenant-context-aware request bootstrap and API endpoint configuration.
4. Ensure fallbacks for SSE interruption/timeouts are visible to users.
5. Keep package independently buildable and deployable.

## TestingAgent Instructions

1. Add tests for SSE stream handling, cancellation/retry behavior, and citation rendering.
2. Add integration-style tests using mocked streaming responses.
3. Validate `lint`, `test`, and `build` commands for the new package.
4. Validate mobile and desktop breakpoints for query and citation panels.

## SecurityGovAgent Instructions

1. Verify tenant context handling is explicit and not optional in query requests.
2. Validate no sensitive trace data is exposed beyond intended UI fields.
3. Check that frontend error handling does not leak internal stack/service details.
4. Review CSP/security header assumptions in deployment notes.

## Acceptance Criteria (Measurable)

1. `search-query-ui/` package exists and `npm run build` succeeds.
2. SSE streaming response is rendered incrementally and final citation list is shown.
3. Frontend tests pass with no failures.
4. Deployment docs/scripts include a reproducible path to run and serve the UI.

## Suggested Validation Commands

```bash
cd search-query-ui && npm ci && npm run lint && npm run test && npm run build

go test ./... -run Retrieval|Rerank|Citation -count=1
```

## Execution Tracking

- [x] Started
- [x] Completed
- Evidence:
  - Coding PASS: `search-query-ui/` created with React + Vite, tenant bootstrap, query input, streaming answer panel, citation list, trace panel, and stream interruption/retry state.
  - Frontend validation PASS: `npm --prefix search-query-ui run lint`; `npm --prefix search-query-ui run test`; `npm --prefix search-query-ui run build`.
  - Backend compatibility PASS: `go test ./... -run 'Retrieval|Rerank|Citation' -count=1`.
  - SecurityGov PASS: `scripts/securitygov_review.sh 'E4-T2|Retrieval|Rerank|Citation|Frontend|React|Vite|SSE'`.
  - Deployment + health PASS: `scripts/deploy_sync_health.sh` (service `search-query-ui` running and exposed on `:14174`; stack checks PASS).
  - Git flow executed: `scripts/git_task_flow.sh create-branch E4-T2 search-query-ui-package` and merge to `dev`.
- Notes:
  - Stream parser supports JSON `data:` frames and plain token fallback frames.
