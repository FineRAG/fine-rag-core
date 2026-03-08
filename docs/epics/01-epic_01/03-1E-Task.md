# E1-T3 API Key Auth and Rate-Limit Enforcement

## Objective

Deliver tenant API-key authentication (revoke-only lifecycle in v1) and enforce configured global and per-tenant throughput limits.

## Scope

1. API-key verification flow and revoked-key behavior.
2. Auth decision recording for audit events.
3. Per-tenant default quota/rate policy (`2 RPS` steady, `4 RPS` burst).
4. Global burst protection (`600 RPS`) and clear rejection semantics.

## Dependencies

1. Upstream required: `E1-T2`.
2. Downstream blocked by this task: `E2-T1`, `E3-T1`.
3. Parallelization: can unlock `E2-T1` and early `E3-T1` prep in parallel after completion.

## Acceptance Criteria

1. Invalid and revoked keys are consistently denied.
2. Auth success/failure is auditable with tenant and request identifiers.
3. Per-tenant and global limits enforce configured thresholds under test load.
4. Rate-limit denials preserve tenant isolation and do not affect unrelated tenant quotas.

## Validation Commands

```bash
# Auth policy and key lifecycle checks
go test ./... -run Auth|APIKey -count=1

# Rate-limit and quota enforcement checks
go test ./... -run RateLimit|Quota -count=1
```

## Execution Tracking

- [ ] Started
- [ ] Completed
- Evidence:
- Notes:
