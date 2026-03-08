# E2-T1 Ingestion Profiling and Metadata Baseline

## Objective

Create deterministic ingestion profiling and metadata capture required for governance decisions and downstream indexing.

## Scope

1. Profile extraction for structure/format quality signals and source characteristics.
2. Metadata schema fields for tenant, checksum, source, and lifecycle class.
3. Deterministic profile output behavior for repeated identical inputs.
4. Persisted job-state baseline required by governance and worker pipelines.

## Dependencies

1. Upstream required: `E1-T3`.
2. Downstream blocked by this task: `E2-T2`, `E2-T3`.
3. Parallelization: may run in parallel with `E3-T1` planning preparations only.

## Acceptance Criteria

1. Profile output is deterministic for identical fixtures.
2. Metadata includes required tenant and governance fields.
3. Invalid payloads are classified with explicit error reasons.
4. Profiling outcomes are available to policy engine input contracts.

## Validation Commands

```bash
# Profiling behavior and determinism checks
go test ./... -run IngestionProfile|Profiler -count=1

# Metadata contract checks
go test ./... -run MetadataSchema -count=1
```

## Execution Tracking

- [ ] Started
- [ ] Completed
- Evidence:
- Notes:
