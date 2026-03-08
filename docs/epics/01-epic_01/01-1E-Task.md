# E1-T1 Core Contracts and Service Skeleton

## Objective

Define stable, tenant-aware contracts and module boundaries for ingestion, retrieval, governance, storage adapters, and observability interfaces.

## Scope

1. Contract definitions for tenant context, auth claims, and request metadata.
2. DTO/interface contracts for ingestion jobs, retrieval queries, rerank requests, and audit events.
3. Service boundary definitions aligned to architecture modules in `docs/system_design.md`.
4. Initial package layout and dependency direction rules.

## Dependencies

1. Upstream: none.
2. Downstream blocked by this task: `E1-T2`, `E1-T3`, `E2-T1`, `E3-T1`.
3. Parallelization: not parallelizable with other E1 tasks.

## Acceptance Criteria

1. Contract and boundary documentation maps each requirement stream to a named interface/module.
2. Tenant identifier is mandatory in all relevant request and persistence contracts.
3. Auth and rate-limit extension points are defined without breaking tenant isolation requirements.
4. A dependency map exists showing allowed import direction and forbidden cyclic coupling.

## Validation Commands

```bash
# Validate compile-time consistency of foundation packages
go test ./... -run Contract -count=1

# Optional static analysis pass for architecture layering
go test ./... -run Architecture -count=1
```

## Execution Tracking

- [x] Started
- [x] Completed
- Evidence:
	- Added Go module and E1-T1 package skeleton with tenant-aware contracts and service boundaries:
		- `go.mod`
		- `internal/contracts/contracts.go`
		- `internal/services/ingestion/service.go`
		- `internal/services/retrieval/service.go`
		- `internal/services/governance/service.go`
	- Added architecture dependency direction documentation:
		- `docs/architecture/dependency-map.md`
	- Added validation tests with required naming patterns:
		- `internal/contracts/contract_test.go`
		- `internal/architecture/architecture_test.go`
	- Validation command results (2026-03-08):
		- `go test ./... -run Contract -count=1` -> PASS
		- `go test ./... -run Architecture -count=1` -> PASS
- Notes:
	- E1-T1 scope implemented only; no middleware/auth runtime logic from E1-T2/E1-T3 was added.
