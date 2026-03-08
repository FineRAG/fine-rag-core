# E1-T1 Dependency Direction Map

Date: 2026-03-08
Task: E1-T1

## Package Boundaries

1. `internal/contracts`
   - Purpose: tenant-aware core DTOs and extension-point interfaces.
   - Imports allowed: standard library only.
2. `internal/services/ingestion`
   - Purpose: ingestion orchestration service boundary.
   - Imports allowed: `internal/contracts`, standard library.
3. `internal/services/retrieval`
   - Purpose: retrieval and rerank service boundary.
   - Imports allowed: `internal/contracts`, standard library.
4. `internal/services/governance`
   - Purpose: governance/policy and audit boundary.
   - Imports allowed: `internal/contracts`, standard library.

## Dependency Direction Rules

1. Contracts are the innermost layer and cannot import service packages.
2. Service boundary packages may import contracts but must not import each other.
3. Adapter/API implementations (future tasks) must depend on service interfaces, never the reverse.
4. Cyclic imports are forbidden across all internal modules.

## Allowed Dependency Graph

- `internal/contracts` -> `(stdlib)`
- `internal/services/ingestion` -> `internal/contracts`
- `internal/services/retrieval` -> `internal/contracts`
- `internal/services/governance` -> `internal/contracts`

## Forbidden Examples

- `internal/contracts` -> `internal/services/*`
- `internal/services/ingestion` -> `internal/services/retrieval`
- `internal/services/retrieval` -> `internal/services/governance`
- `internal/services/governance` -> `internal/services/ingestion`
