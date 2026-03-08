# Review-Driven Workflow Guide

This project uses file-based review gates to accept comments and control progression.

## Review Files
- Requirements: `.ai/reviews/requirements.review.md`
- Design: `.ai/reviews/design.review.md`
- Epic plan: `.ai/reviews/epic.review.md`
- Task packs: `.ai/reviews/tasks.review.md`

## How to Add Comments
1. Open the stage review file.
2. Add a row in the `Comments` table.
3. Set `Status` to `Open`.
4. Ask the stage agent to revise using that review file.

## How to Proceed (Button Equivalent)
Use the checkbox under `Proceed Gate`:
- unchecked (`[ ]`) = do not move to next stage
- checked (`[x]`) = next stage is approved to run

## End-to-End Re-trigger Flow
1. Update comments in the relevant review file.
2. Run stage agent to revise only that artifact.
3. Mark proceed checkbox checked after approval.
4. Run next stage agent.
5. Repeat until task packs are approved.
6. Only then run CodingAgent, TestingAgent, SecurityGovAgent, and DeploymentAgent.

## Suggested Invocation Sequence
1. AnalyzerAgent -> `docs/distilled_requirements.md`
2. ArchitectAgent -> `docs/system_design.md`
3. ExecutionManagerAgent -> `docs/epics/00-epic_summary.md` and `docs/epics/**`
4. ExecutionManagerAgent -> orchestrate CodingAgent/Test/SecurityGovAgent/DeploymentAgent for approved tasks
