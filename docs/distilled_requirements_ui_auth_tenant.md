# Distilled Requirements: UI Auth and Tenant Flow for Ingestion + Search

Date: 2026-03-09
Owner: AnalyzerAgent
Status: Draft for review
Related clients: `ingestion-dashboard-ui`, `search-query-ui`

## Executive Summary
- This document defines testable requirements for adding login-gated access and tenant onboarding flows to both UI clients.
- It preserves current session/header behavior (`Authorization`, `X-Tenant-ID`, `X-Request-ID`) and separates frontend-only requirements from backend-dependent assumptions because backend API server implementation is not present in this repository.
- User-requested defaults are captured: username `admin`, password `sk-1234`, default tenant `tenant-1234`.

## Final Decision Table
| Decision ID | Topic | Decision | Status |
| --- | --- | --- | --- |
| D-UI-001 | Login gating | Ingestion UI and Search UI must be behind login page. | Confirmed |
| D-UI-002 | API protection | All ingestion UI API calls must be auth-protected. | Confirmed |
| D-UI-003 | Tenant flow | Post-login flow must allow new-tenant creation and auto-open dashboard when user has exactly one tenant. | Confirmed |
| D-UI-004 | Defaults | Default user is `admin` / `sk-1234`; default tenant is `tenant-1234`. | Confirmed |
| D-UI-005 | API key UX | API key create/delete actions must be initiated by user through popup UI. | Confirmed |
| D-UI-006 | Ingestion inputs | Ingestion submission must accept URI or local file/folder source. | Confirmed |
| D-UI-007 | Backend boundary | UI requirements must define expected endpoints/contracts only; backend implementation is out of repo scope. | Confirmed |

## Project Goals
- Provide secure, login-gated UX for both ingestion and search clients.
- Reduce user friction in tenant selection by auto-opening when exactly one tenant exists.
- Enable self-service tenant and API key workflows through UI interactions.
- Support ingestion from cloud/object URI and local file/folder selection paths.

## In Scope
- Login page and authenticated session bootstrap for `ingestion-dashboard-ui`.
- Login page and authenticated session bootstrap for `search-query-ui`.
- Tenant selection behavior after successful login, including tenant creation option.
- Popup-based API key creation and deletion flows in ingestion UI.
- Ingestion submission UX supporting URI and local file/folder input modes.
- Frontend handling for auth headers on all protected API calls.

## Out of Scope
- Backend implementation of authentication, tenant registry, file upload, and API key APIs.
- IAM/SSO/OIDC enterprise identity integration.
- Authorization policy engine redesign beyond required API protection behavior.
- Infrastructure-level secret vault rollout.

## Functional Requirements

### Frontend-Only Requirements
- FR-FE-001: Each UI app shall render a login page before any dashboard content is accessible.
- FR-FE-002: On successful login, the UI shall persist an authenticated session in client state and route to tenant resolution flow.
- FR-FE-003: Post-login tenant flow shall show two options when user has zero or multiple tenants: select existing tenant or create new tenant.
- FR-FE-004: If user has exactly one tenant, the UI shall auto-open that tenant dashboard without requiring manual selection.
- FR-FE-005: Ingestion UI shall expose API key creation and deletion actions through explicit popup dialogs requiring user confirmation.
- FR-FE-006: Ingestion submission UI shall provide source mode toggle with exactly two modes: `URI` and `Local`.
- FR-FE-007: In `URI` mode, submission form shall collect source URI and required metadata fields.
- FR-FE-008: In `Local` mode, submission form shall allow file picker and folder picker interactions and show selected items before submission.
- FR-FE-009: Search UI shall enforce the same login gate and tenant resolution flow before query actions are enabled.
- FR-FE-010: Both UIs shall include logout action that clears session and returns user to login page.

### Backend-Dependent Contract Requirements (Expected by UI)
- FR-BE-001: Login API shall validate username/password and return auth token plus tenant list for the authenticated user.
- FR-BE-002: Tenant creation API shall create a tenant for authenticated user and return created tenant metadata.
- FR-BE-003: API key list/create/delete endpoints shall require valid auth token and tenant-scoped authorization.
- FR-BE-004: Ingestion submit endpoint shall accept either URI payload or multipart/local-upload payload representation.
- FR-BE-005: Search stream endpoint shall require valid auth token and tenant context before stream starts.
- FR-BE-006: Unauthorized or expired auth responses shall use explicit status codes so UI can redirect to login.

## Existing Session/Header Compatibility Requirements
- FR-CTX-001: UI request layer shall continue sending `Authorization: Bearer <token or api key>` header for protected endpoints.
- FR-CTX-002: UI request layer shall continue sending `X-Tenant-ID` and `X-Request-ID` for tenant and trace context.
- FR-CTX-003: Session bootstrap shall populate tenant and request IDs before enabling protected actions.

## Non-Functional Requirements
- NFR-001 (Usability): First-time login-to-dashboard path shall complete in <= 3 user actions when exactly one tenant exists.
- NFR-002 (Security): No protected API call shall be sent without auth header in authenticated screens.
- NFR-003 (Resilience): UI shall handle 401/403 responses with deterministic state reset and user-facing re-login prompt.
- NFR-004 (Accessibility): Login, tenant selection, API key popup dialogs, and ingestion source controls shall be keyboard-accessible and screen-reader labeled.
- NFR-005 (Performance): Login page render and route transition to dashboard should complete within 2 seconds on local dev machine under normal conditions.
- NFR-006 (Observability): UI shall emit client-side telemetry events for login success/failure, tenant selection, tenant creation, API key create/delete actions, and ingestion mode used.

## Multi-Tenancy and Isolation Guarantees
- MT-001: UI shall never issue tenant-scoped ingestion/search/API-key requests without explicit active tenant context.
- MT-002: UI shall update active tenant context atomically on tenant switch and ensure subsequent requests use the new tenant only.
- MT-003: UI shall not display cached API key records, ingestion jobs, or search traces from a previous tenant after switching tenant.

## Governance and Compliance Requirements
- GOV-001: Login/auth UX shall support auditability by propagating request IDs to all protected API calls.
- GOV-002: API key values displayed after creation shall be shown once in popup context and not persisted in local storage.
- GOV-003: UI shall avoid logging raw credentials, tokens, or full API key material to browser console or telemetry.

## Security Considerations for Default Credentials and Demo Mode
- SEC-001: Default credentials (`admin` / `sk-1234`) and default tenant (`tenant-1234`) shall be treated as local/demo-only bootstrap values.
- SEC-002: UI shall visibly label sessions authenticated via default credentials as `Demo Mode`.
- SEC-003: UI shall require explicit non-demo configuration flag before allowing production deployment build artifacts.
- SEC-004: In non-demo mode, login form shall not prefill default credentials.
- SEC-005: UI requirements assume backend enforces rejection or rotation policy for default credentials outside demo mode.

## Data Model and Lifecycle States
- DATA-001: Auth session lifecycle states shall be `logged_out`, `authenticating`, `authenticated`, `expired`, `error`.
- DATA-002: Tenant resolution states shall be `unresolved`, `single_tenant_auto_opened`, `tenant_selected`, `tenant_created`.
- DATA-003: Ingestion source mode states shall be `uri_mode` and `local_mode`.
- DATA-004: API key popup action states shall be `idle`, `create_dialog_open`, `delete_dialog_open`, `submitting`, `success`, `failure`.

## Integration Requirements
- INT-001: `ingestion-dashboard-ui` shall route login success to ingestion dashboard only after active tenant context is set.
- INT-002: `search-query-ui` shall route login success to query page only after active tenant context is set.
- INT-003: Existing ingestion endpoints under `/api/v1/ingestion/jobs` and API key endpoints under `/api/v1/tenants/{tenantId}/api-keys` shall remain supported by UI request clients.
- INT-004: Existing search stream endpoint `/api/v1/search/stream` shall remain supported by search UI client.
- INT-005: Request/response schema changes needed for login/tenant creation/local upload shall be versioned and backward compatibility impact documented.

## Deployment and Runtime Constraints
- DEP-001: Both UIs must remain deployable as static frontend bundles with runtime API base URL environment variables.
- DEP-002: Backend behavior is represented as expected contracts only because backend server implementation is not present in this repo.
- DEP-003: Demo-mode defaults must be configurable through environment variables and disabled by explicit configuration for non-demo deployment.

## Test and Acceptance Criteria

### Stream A: Shared Login and Tenant Resolution
- AC-A-001: Opening either UI root URL shows login page and hides dashboard components before authentication.
- AC-A-002: With valid login and exactly one tenant in login response, UI auto-navigates to tenant dashboard and sets active tenant context.
- AC-A-003: With valid login and multiple tenants, UI presents tenant selection and create-tenant option.
- AC-A-004: Logout action clears auth/session state and blocks protected API actions until re-login.

### Stream B: Ingestion Dashboard Security and UX
- AC-B-001: All ingestion dashboard API requests include `Authorization`, `X-Tenant-ID`, and `X-Request-ID` headers after login.
- AC-B-002: Attempting ingestion/API-key operations without valid session is blocked client-side.
- AC-B-003: API key create action opens popup, requires user confirmation, and only then sends create request.
- AC-B-004: API key delete action opens popup, requires user confirmation, and only then sends delete request.
- AC-B-005: Ingestion form supports URI input mode and Local input mode; submission payload path is validated for both modes.

### Stream C: Search Query UI Security and UX
- AC-C-001: Search UI is inaccessible before login and tenant resolution.
- AC-C-002: Search stream request includes `Authorization`, `X-Tenant-ID`, and `X-Request-ID` headers after login.
- AC-C-003: 401/403 from search endpoint returns user to login prompt with clear status message.

### Stream D: Demo-Security Behavior
- AC-D-001: In demo/local mode, login form may show defaults `admin` / `sk-1234` and tenant `tenant-1234`.
- AC-D-002: In non-demo mode, defaults are not prefilled and UI shows no demo badge.
- AC-D-003: Browser console and telemetry output contain no raw password/token/default secret values during login and API key flows.

## Risks and Assumptions
- RISK-001: Without backend contract implementation in repo, end-to-end auth flow cannot be fully validated here.
- RISK-002: Local folder upload behavior can vary by browser support for directory selection.
- ASM-001: Backend will expose login, tenant-list, tenant-create, and auth error semantics compatible with UI requirements.
- ASM-002: Backend will enforce tenant authorization consistency with `X-Tenant-ID` and auth token identity.

## Open Items (Unavoidable)
- OQ-001 (Owner: Product + Backend, Deadline: 2026-03-12): What is the canonical login API response schema for tenant list and auth token fields?
1. Option A: `{ token, user, tenants[] }`
2. Option B: `{ accessToken, expiresAt, memberships[] }`
3. Option C: Other schema (must be finalized and documented)

- OQ-002 (Owner: Backend, Deadline: 2026-03-12): For local file/folder ingestion, which transport contract is required?
1. Option A: Multipart file upload directly to backend.
2. Option B: Presigned URL flow with client-side upload then ingest-by-reference.
3. Option C: Zip-and-upload contract for folder mode.

## Decision Log Appendix
| Date | Item | Decision/Update | Status |
| --- | --- | --- | --- |
| 2026-03-09 | UI gating | Both UIs require login gate before dashboard access. | Confirmed |
| 2026-03-09 | Tenant post-login flow | Auto-open if exactly one tenant; otherwise select/create tenant. | Confirmed |
| 2026-03-09 | Default credentials handling | Defaults kept for demo/local mode with explicit security constraints. | Confirmed |
| 2026-03-09 | Backend boundary | Requirements split into frontend-only behavior vs backend-dependent contracts. | Confirmed |