# E2-T2 Governance Gatekeeper and Policy Outcomes

## Objective

Apply policy evaluation (including PII and residency controls) and produce auditable ingestion decisions before indexing.

## Scope

1. Policy decision engine for `approved`, `quarantine`, and `rejected` outcomes.
2. PII redaction enforcement before downstream model/index calls.
3. Residency lock checks for `ap-south-1` compliance.
4. Audit event generation for policy decision rationale.

## Dependencies

1. Upstream required: `E2-T1`.
2. Downstream blocked by this task: `E2-T3`, `E3-T2`.
3. Parallelization: sequential after `E2-T1`; can overlap with E3 observability prep after partial readiness.

## Acceptance Criteria

1. Policy decisions are deterministic and reproducible for test fixtures.
2. PII handling paths are validated for redaction-before-index behavior.
3. Non-compliant residency conditions are denied with auditable reason codes.
4. Decision logs include tenant, artifact, policy code, and timestamp metadata.

## Validation Commands

```bash
# Policy outcome and gate behavior checks
go test ./... -run PolicyGate|Governance -count=1

# Compliance-focused checks
go test ./... -run PII|Residency -count=1
```

## Execution Tracking

- [ ] Started
- [ ] Completed
- Evidence:
- Notes:
