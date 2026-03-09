# System Design Addendum: UI Auth + Tenant Flow (Ingestion + Search)

Date: 2026-03-09
Status: Draft for implementation
Source requirements: `docs/distilled_requirements_ui_auth_tenant.md`
Applies to: `ingestion-dashboard-ui`, `search-query-ui`

## 1) Design Summary

This addendum defines a minimal-change, implementation-oriented frontend architecture to add login-gated access, tenant selection/creation, API-key popup workflows, and URI/local ingestion mode support across both existing React apps.

Because backend auth/tenant APIs are not implemented in this repository, design is explicitly dual-mode:

- `demo/local mode` (immediate UX): browser-persisted auth + tenant registry + key metadata for local development and demos.
- `backend-integrated mode` (future): same UI state machine, with provider adapters switched to HTTP contracts.

Key outcome: preserve existing request header behavior (`Authorization`, `X-Tenant-ID`, `X-Request-ID`) while introducing auth-first routing and tenant resolution before protected operations.

## 2) Requirement-to-Design Traceability

| Requirement | Design decision | Concrete implementation target |
| --- | --- | --- |
| D-UI-001, FR-FE-001, FR-FE-009 | Both apps render login gate first; dashboard/query screen requires authenticated session. | Add auth gate state in `ingestion-dashboard-ui/src/App.tsx` and `search-query-ui/src/App.tsx`. |
| D-UI-003, FR-FE-003, FR-FE-004 | Tenant resolver after login: auto-open if exactly one tenant; otherwise select/create view. | Add `TenantResolver` component in each app and shared tenant resolver logic module per app. |
| D-UI-004, SEC-001..SEC-004 | Demo bootstrap defaults (`admin`/`sk-1234`, `tenant-1234`) only in demo mode; visible demo badge and production guard. | Add env-gated demo config in each app (`VITE_UI_DEMO_MODE`, default true local), no prefill when false. |
| D-UI-005, FR-FE-005, GOV-002 | API key create/revoke via confirmation popup; key value displayed once and never persisted. | Add popup state machine in ingestion app; store created key value only in in-memory React state. |
| D-UI-006, FR-FE-006..FR-FE-008 | Ingestion source mode toggle (`URI` vs `Local`), with payload adapter for each mode. | Extend ingestion form in `ingestion-dashboard-ui/src/App.tsx` and `src/api.ts` payload serializer. |
| FR-CTX-001..003, AC-B-001, AC-C-002 | Protected API client injects `Authorization`, `X-Tenant-ID`, `X-Request-ID`; blocks unauthenticated requests. | Keep existing header builders in both `src/api.ts`; add centralized auth session guard wrapper. |
| FR-BE-001..006, D-UI-007 | Backend boundary represented as contract seams only, with adapter switch by mode. | Define `AuthTenantProvider` and `ApiKeyProvider` interfaces; provide `browserStoreProvider` and `httpProvider`. |
| AC-D-001..003, GOV-003 | Demo security caveats enforced in UI and telemetry redaction. | Redact auth/key values in logs/events; render `Demo Mode` label on active default bootstrap sessions. |
| NFR-003, AC-C-003 | 401/403 deterministic reset to login with user-facing message. | Add fetch response interceptor helper in each app that triggers logout/reset on auth failure. |
| NFR-004 | Keyboard-accessible login, tenant resolver, and popups. | Add semantic form labels, focus traps for dialogs, and escape/enter handling in popup component. |

## 3) Cost/Infra Optimization Recommendations

This addendum is frontend-scoped and intentionally avoids adding net-new infrastructure in v1.

1. Right-size by mode:
- `v1`: browser-based demo store + existing static builds; no extra services.
- `v1.1+`: switch provider adapters to backend APIs with no major UI rewrite.
- Trade-off: fastest delivery now vs temporary duplication between demo and backend providers.

2. Reuse existing app structure (single `App.tsx` per UI) before introducing shared package:
- Keep changes local to each app (`src/App.tsx`, `src/api.ts`, `src/types.ts`, tests).
- Trade-off: some duplicated auth/tenant logic between apps vs reduced refactor risk.

3. Control observability cardinality from day one:
- Emit telemetry with bounded event names and hashed request IDs.
- Do not emit full URIs with user-identifying fragments in default event payloads.
- Trade-off: lower analytics detail vs safer cost and privacy profile.

4. Local ingestion mode contract strategy:
- `v1`: model local mode as metadata-only placeholder (`local://<name>`) in demo mode.
- `v1.1+`: real multipart/presigned upload once backend contract finalized.
- Trade-off: immediate UX coverage vs deferred true upload transport.

## 4) Open Risks and Unresolved Decisions

1. Login response schema remains open (`OQ-001`): token + tenant fields not finalized.
2. Local file/folder backend transport remains open (`OQ-002`): multipart vs presigned vs zip-and-upload.
3. Production safety guard enforcement point is open: UI build-time gate only, or UI + backend runtime assertion.
4. Tenant creation conflict handling semantics are open (duplicate name/slug and idempotency key strategy).

## 5) Final Design Document Write Confirmation

Design addendum authored at `docs/system_design_ui_auth_tenant.md` and scoped to current repository structure.

---

## Architecture Overview and Boundaries

### Existing UI Baseline (as-is)

- `ingestion-dashboard-ui/src/App.tsx` currently starts directly at tenant/API-key session bootstrap and ingestion dashboard.
- `search-query-ui/src/App.tsx` currently starts directly at tenant/API-key session bootstrap and query panel.
- Both apps already have request clients that attach `Authorization`, `X-Tenant-ID`, and `X-Request-ID` in `src/api.ts`.

### Target Boundary (to-be)

Each app will adopt the same front-end state pipeline:

1. `LoginGate`
2. `TenantResolver`
3. `AuthenticatedWorkspace` (existing dashboard/query views)

Cross-cutting module seam per app:

- `authTenantProvider`:
1. demo provider: browser store implementation.
2. http provider: backend contract implementation.

No backend code is introduced by this addendum.

## Data Flow: Auth, Tenant, Ingestion, Search, Governance Signals

### A. Login + Tenant Resolution

1. User lands at app root and sees login page.
2. Login submit calls provider `login(username, password)`.
3. Provider returns auth token, user identity, and tenant memberships.
4. If tenant count is 1, tenant is auto-selected and app opens workspace.
5. If tenant count is 0 or >1, tenant resolver screen appears with select/create actions.
6. On tenant create success, active tenant is set atomically and workspace opens.

### B. Protected API Behavior

1. Before each protected call, session guard verifies `authToken`, `tenantId`, `requestId`.
2. Request headers include `Authorization`, `X-Tenant-ID`, `X-Request-ID`.
3. If response is 401/403, app clears auth state and routes to login with message.

### C. API-Key Popup UX (Ingestion App)

1. User clicks `Create Key` -> create dialog opens.
2. Confirm action submits create call via provider/client.
3. Returned key secret value shown once in success dialog pane; not persisted in storage.
4. Revoke flow uses separate confirmation popup before delete request.
5. Popup state machine: `idle -> create_dialog_open|delete_dialog_open -> submitting -> success|failure`.

### D. Ingestion Submission Model (`URI` vs `Local`)

1. Source mode selector defaults to `URI`.
2. `URI` mode sends JSON payload with source URI + metadata/checksum.
3. `Local` mode captures selected file/folder entries in UI and sends mode-specific payload through serializer seam.
4. In demo mode, local submission emits synthetic `local://` source references and marks ingestion as queued for UI continuity.
5. In backend mode, local submission path maps to finalized upload contract (open decision).

### E. Query Flow (Search App)

1. Query panel is disabled until auth + tenant are resolved.
2. Stream request continues using existing SSE path (`/api/v1/search/stream`).
3. On token/citation/trace events, current rendering logic remains unchanged.

## Tenant Isolation and Security Model (Frontend Scope)

### Isolation Guarantees

- Active tenant is single-valued client state; tenant switch is atomic.
- On switch, tenant-scoped cached data (jobs, keys, citations/trace) is cleared.
- No tenant-scoped request is sent without active tenant context.

### Default Bootstrap Security Caveats

- Default credentials and tenant are demo-only values:
1. user: `admin`
2. password: `sk-1234`
3. default tenant: `tenant-1234`
- UI must visibly tag these sessions with `Demo Mode` badge.
- Non-demo builds must disable prefilled defaults.
- UI must never log raw password, token, or key secret values.

### Frontend-Only vs Backend-Integrated Modes

#### Frontend-Only Mode (`VITE_UI_DEMO_MODE=true`)

- Auth and tenant data from browser store (`localStorage` for non-secret profile; `sessionStorage` for token/session).
- API key records in browser store; created key secret kept only in memory for current render cycle.
- Ingestion/search calls may be simulated or partially live depending on endpoint availability.

#### Backend-Integrated Mode (`VITE_UI_DEMO_MODE=false`)

- Login/tenant/api-key operations call backend contracts.
- Session token and tenant memberships sourced from backend responses.
- Upload/search/ingestion flows use existing API base URLs with auth enforcement.

## Backend Contract Seams (Future Integration)

Proposed TypeScript interfaces to add in each app (`src/authTenantProvider.ts`):

```ts
export type LoginResult = {
  authToken: string
  user: { userId: string; username: string }
  tenants: Array<{ tenantId: string; name: string }>
  demoMode: boolean
}

export interface AuthTenantProvider {
  login(username: string, password: string): Promise<LoginResult>
  logout(): Promise<void>
  listTenants(): Promise<Array<{ tenantId: string; name: string }>>
  createTenant(input: { name: string }): Promise<{ tenantId: string; name: string }>
}

export interface ApiKeyProvider {
  listApiKeys(tenantId: string): Promise<Array<{ keyId: string; label: string; createdAt: string }>>
  createApiKey(tenantId: string, label: string): Promise<{ keyId: string; value: string; createdAt: string }>
  revokeApiKey(tenantId: string, keyId: string): Promise<void>
}
```

Expected HTTP endpoints (contracts only):

- `POST /api/v1/auth/login`
- `POST /api/v1/auth/logout`
- `GET /api/v1/tenants`
- `POST /api/v1/tenants`
- `GET /api/v1/tenants/{tenantId}/api-keys`
- `POST /api/v1/tenants/{tenantId}/api-keys`
- `DELETE /api/v1/tenants/{tenantId}/api-keys/{keyId}`

## Runtime and Deployment Topology (UI Addendum)

- Keep current static Vite build + Nginx hosting model.
- Add environment flags only (no new infra):
1. `VITE_UI_DEMO_MODE=true|false`
2. `VITE_DEFAULT_DEMO_USERNAME=admin`
3. `VITE_DEFAULT_DEMO_PASSWORD=sk-1234`
4. `VITE_DEFAULT_DEMO_TENANT=tenant-1234`
- Production deploy pipeline must explicitly set `VITE_UI_DEMO_MODE=false`.

## SLO and Scaling Strategy (UI Scope)

- Login page initial render <= 2s on local dev baseline.
- Single-tenant login-to-dashboard path <= 3 user actions.
- 401/403 handling state reset <= 1 interaction to return user to login page.
- Client API guard: 100% protected calls include required auth and tenant headers.

## Failure Modes and Recovery Strategy

1. Expired/invalid auth token:
- Trigger deterministic logout and banner: `Session expired. Please sign in again.`

2. Tenant list load failure:
- Keep user authenticated, show retry CTA, avoid entering workspace without tenant.

3. API key operation failure:
- Keep dialog open with actionable error and retry option; no optimistic destructive update on revoke.

4. Local folder picker unsupported browser:
- Detect capability and fallback to file-only picker + user guidance.

5. Partial backend availability in backend mode:
- Feature-level degradation flags (disable tenant create/API-key popups if endpoint unavailable).

## CI/CD and Quality Gates (Frontend)

### Required Tests Per App

- Unit:
1. auth state transitions (`logged_out`, `authenticating`, `authenticated`, `expired`, `error`)
2. tenant resolver transitions (`unresolved`, `single_tenant_auto_opened`, `tenant_selected`, `tenant_created`)
3. popup state machine transitions for API key create/revoke
4. ingestion serializer by source mode (`uri_mode`, `local_mode`)

- Integration (existing Vitest + RTL pattern):
1. login gate blocks dashboard before auth
2. one-tenant auto-open path
3. multi-tenant select/create path
4. header injection assertion for protected requests
5. 401/403 forced logout behavior
6. ingestion URI mode and local mode submission path

- Security regression tests:
1. no credentials/token/key secrets in console spy output
2. demo defaults absent when non-demo mode enabled

### Quality Gates

- `npm test` must pass in both app folders.
- No direct `fetch` usage for protected endpoints outside API wrapper modules.
- Build fails if production config enables demo mode.

## Minimal-Change Implementation Plan (Existing React Structures)

### Stream 1: Shared Auth + Tenant Resolver (both apps)

1. Update `ingestion-dashboard-ui/src/types.ts`:
- add `AuthSession`, `TenantMembership`, `AuthState`, `TenantResolutionState`.

2. Update `search-query-ui/src/types.ts` similarly.

3. Add `src/authTenantProvider.ts` in each app:
- demo/browser implementation now.
- http provider stubs with endpoint mapping for future integration.

4. Modify `src/App.tsx` in each app:
- wrap current UI in `authenticated workspace` section.
- prepend login and tenant resolver views.
- keep existing workspace controls mostly unchanged.

### Stream 2: API Client Hardening

1. Update `ingestion-dashboard-ui/src/api.ts` and `search-query-ui/src/api.ts`:
- support bearer auth token source from new auth state.
- centralize 401/403 handler callback.
- preserve existing header keys and endpoint paths.

2. Add `requestId` generation utility per session bootstrap if absent.

### Stream 3: Ingestion UX Extension

1. Extend `ingestion-dashboard-ui/src/App.tsx` ingestion form:
- mode toggle: `URI` or `Local`.
- local file/folder selectors + selected-items preview list.

2. Extend `ingestion-dashboard-ui/src/api.ts` payload serializer:
- `serializeIngestionPayload(mode, uriInput, localSelection, checksum)`.

### Stream 4: API-Key Popup UX

1. Add popup/dialog component (same app, local file):
- create popup with confirm/cancel.
- revoke popup with confirm/cancel.

2. Replace direct create/revoke buttons in `ingestion-dashboard-ui/src/App.tsx` with popup-triggered actions.

### Stream 5: Test Updates

1. Update `ingestion-dashboard-ui/src/App.test.tsx` to include:
- login gate, tenant auto-open, popup confirmations, local mode submission.

2. Update `search-query-ui/src/App.test.tsx` to include:
- login gate, tenant flow, 401/403 reset.

3. Extend API helper tests in both apps for:
- header guarantees, auth-guard behavior, mode serializers.

## Implementation Phases Aligned to Task Streams

### Phase 1 (1-2 days): Auth/Tenant shell

- Add auth/tenant models and demo provider.
- Introduce login gate + tenant resolver in both apps.
- Keep existing ingestion/query core unchanged behind gate.

### Phase 2 (1-2 days): Ingestion/API-key UX

- Add URI/local mode and serializers.
- Add API key create/revoke popup flows.
- Add demo badge and non-demo guardrails.

### Phase 3 (1 day): Test hardening + backend seams

- Complete required tests for both apps.
- Add explicit provider interfaces and backend endpoint stubs.
- Document unresolved contract decisions for backend handoff.
