# Deployment Status

Date: 2026-03-09
Owner: ExecutionManagerAgent

| Task ID | Branch | Deploy Stage | Sync Status | EC2 Log Status | PR to dev |
|---|---|---|---|---|---|
| E1-T1 | historical | completed (already merged) | completed | available in prior deploy logs | merged |
| E1-T2 | historical | completed (already merged) | completed | available in prior deploy logs | merged |
| E1-T3 | `feature/E1-T3-e1-t3-e2-t1-auth-profiler` | completed | completed (rsync + compose deploy succeeded) | health-check PASS (`scripts/check_stack.sh`) | merged to dev (no gh CLI PR) |
| E2-T1 | `feature/E1-T3-e1-t3-e2-t1-auth-profiler` | completed | completed (rsync + compose deploy succeeded) | health-check PASS (`scripts/check_stack.sh`) | merged to dev (no gh CLI PR) |
| E2-T2 | `feature/E2-T2-governance-gatekeeper` | completed | completed (rsync + compose deploy succeeded) | health-check PASS (`scripts/check_stack.sh`) | merged to dev (no gh CLI PR) |
| E2-T3 | `feature/E2-T3-async-queue-worker-indexing` | completed | completed (rsync + compose deploy succeeded) | health-check PASS (`scripts/check_stack.sh`) | merged to dev (no gh CLI PR) |
| E3-T1 | `feature/E3-T1-retrieval-api-rerank` | completed | completed (rsync + compose deploy succeeded) | health-check PASS (`scripts/check_stack.sh`) | merged to dev (no gh CLI PR) |
| E3-T2 | `feature/E3-T2-slo-security-governance-validation` | completed | completed (rsync + compose deploy succeeded) | health-check PASS (`scripts/check_stack.sh`) | merge verified (`dev` already up to date; no gh CLI PR) |
| E3-T3 | `feature/E3-T3-operability-release-readiness-stage-handoff` | completed | completed (rsync + compose deploy succeeded) | health-check PASS (`scripts/check_stack.sh`) | merged to dev (manual PR handoff; gh CLI missing) |
| E4-T1 | `feature/E4-T1-ingestion-dashboard-ui-package` | completed | completed (rsync + compose deploy succeeded; UI image built remotely) | health-check PASS (`scripts/check_stack.sh`) with `ingestion-dashboard-ui` service on `:14173` | PR manual (gh missing) |
| E4-T2 | `feature/E4-T2-search-query-ui-package` | completed | completed (rsync + compose deploy succeeded; UI image built remotely) | health-check PASS (`scripts/check_stack.sh`) with `search-query-ui` service on `:14174` | PR manual (gh missing) |
