# Design Review Gate
Stage: design
Owner: Architecture
Updated: 2026-03-08

## Proceed Gate
- [x] Proceed to Epic Planning

## Decision Summary
- Status: pending
- Reviewer:
- Reviewed artifact: `docs/system_design.md`

## Comments
| ID | Priority | Section | Comment | Proposed Change | Status |
|---|---|---|---|---|---|
| DS-001 | High | Whole Document | System design draft was incomplete for implementation and missed explicit coverage for cost model/trade-offs, failure recovery, CI/CD quality gates, and requirement traceability. | Revise `docs/system_design.md` to include full architecture boundaries, end-to-end data flow with governance, tenant isolation/security model, runtime topology, measurable SLO/scaling plan, cost optimizations with alternatives and trade-offs, failure/recovery strategy, CI/CD gates, and phased implementation mapping to task streams. | Resolved |

## Resolution Log
| ID | Resolved By | Resolution | Date |
|---|---|---|---|
| DS-001 | ArchitectAgent | Reworked system design into an implementation-ready architecture with full required coverage, measurable targets, trade-offs, and requirement-to-design traceability. | 2026-03-08 |
| CMT-1 | ArchitectAgent | Added two explicit frontend services (React + Vite) for ingestion dashboard and tenant search/query UI in architecture boundaries and topology. | 2026-03-08 |
| CMT-2 | ArchitectAgent | Added SSE streaming requirement for AI-generated responses in service boundaries and retrieval flow. | 2026-03-08 |
| CMT-3 | ArchitectAgent | Expanded cost model to explicitly prioritize embedding/generation/reranker token usage as a primary cost driver. | 2026-03-08 |
| CMT-4 | ArchitectAgent | Locked v1 queue choice to AWS SQS and documented trade-off plus v1.1 alternative path. | 2026-03-08 |
| CMT-5 | ArchitectAgent | Added CI/CD deploy script contract for rsync + docker compose deployment on EC2 including frontend/backend/observability stack startup. | 2026-03-08 |
| CMT-6 | ArchitectAgent | Added observability minimums covering embedding/input/output tokens, TTFT, and RED metrics. | 2026-03-08 |
| CMT-7 | ArchitectAgent | Added managed reranker provider suggestions with measurable SLA, routing, and cost-guardrail recommendations. | 2026-03-08 |
| CMT-8 | ArchitectAgent | Added API-key create/revoke capability to ingestion dashboard service boundary and retained security guardrail decision as open. | 2026-03-08 |
| CMT-9 | ArchitectAgent | Updated topology to hosted PostgreSQL and hosted Milvus with provider credential model and DR obligation checks. | 2026-03-08 |

## Next Run Instructions
- If proceed remains unchecked, ArchitectAgent must only revise design.
- When proceed is checked, ExecutionManagerAgent may run.

## Imported Inline Comments (2026-03-08T16:59:13.716Z)
- [x] CMT-1 line 28: I don__SQUOTE__t see any frontend service. We need to have two react + vite frameworks for ingestion control and dashboard UI, and search/query UI fro end user
- [x] CMT-2 line 30: in AI generated response we need to stream response to user
- [x] CMT-3 line 129: AI token usage and cost
- [x] CMT-4 line 144: lets use sqs in v1
- [x] CMT-5 line 176: need to have a script which will resync, run docker compose, deploy system on EC2 instance. start all containers, frontend, backend, dbs, grafana openlit prometheus etc etc
- [x] CMT-6 line 202: need to have embedding token count, AI model input & output token count, TTFT, RED
- [x] CMT-7 line 229: Provide suggestions based on current design and requirements so far
- [x] CMT-8 line 230: User can create API-key or revoke API-Key from ingestion dashboard
- [x] CMT-9 line 231: hosted platform will be used. i will provide creds for postgres and milvus
