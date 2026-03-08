# E1-T2 Tenant Context Middleware and Isolation Guards

## Objective

Implement strict tenant extraction, propagation, and enforcement so every request path is tenant-scoped by default and cannot be bypassed.

## Scope

1. Middleware contract for tenant context extraction from authenticated request metadata.
2. Request rejection behavior for missing or malformed tenant context.
3. Immutable tenant context propagation to service and repository layers.
4. Guardrails that prevent unscoped repository queries and writes.

## Dependencies

1. Upstream required: `E1-T1`.
2. Downstream blocked by this task: `E1-T3`, `E2-T1`, `E3-T1`.
3. Parallelization: sequential after `E1-T1`; no overlap with `E1-T3` completion criteria.

## Acceptance Criteria

1. Requests with missing tenant context are rejected with deterministic API behavior.
2. Repository access paths fail if tenant scope is absent.
3. Integration test scenarios cover cross-tenant leakage attempts and prove rejection.
4. Tenant context is logged/traced in observability metadata without leaking sensitive values.

## Validation Commands

```bash
# Middleware and context propagation tests
go test ./... -run TenantContext -count=1

# Isolation regression test suite
go test ./... -run Isolation -count=1
```

## Execution Tracking

- [x] Started
- [x] Completed
- Evidence:
	- Implemented tenant context extraction middleware with deterministic rejection for missing/malformed metadata and immutable context injection:
		- `internal/middleware/tenant_context.go`
		- `internal/middleware/tenant_context_test.go`
	- Implemented tenant context propagation and isolation contract helpers:
		- `internal/contracts/contracts.go`
		- `internal/contracts/contract_test.go`
	- Implemented repository scope guardrails that fail unscoped/cross-tenant operations:
		- `internal/repository/tenant_guard.go`
		- `internal/repository/tenant_guard_test.go`
	- Validation command results (2026-03-08):
		- `go test ./... -run TenantContext -count=1` -> PASS
		- `go test ./... -run Isolation -count=1` -> PASS
- Notes:
	- Scope constrained to E1-T2: no E1-T3 API-key auth or runtime rate-limit behavior implemented.
