---
description: "Run a review-gated cycle for requirements -> design -> epic -> task packs using proceed checkboxes in .ai/reviews/*.md."
---
Run only the requested stage and obey review gates.

Inputs:
- Stage: one of `requirements`, `design`, `epic`, `tasks`
- Reviewer comments source: corresponding `.ai/reviews/<stage>.review.md`

Rules:
- Read comments and resolve open items first.
- Do not advance to next stage unless current stage `Proceed Gate` is checked (`[x]`).
- If proceed is unchecked, update only the current stage artifact.
- Use latest non-versioned output files.

Stage mapping:
- `requirements` -> AnalyzerAgent -> `docs/distilled_requirements.md`
- `design` -> ArchitectAgent -> `docs/system_design.md`
- `epic` -> ExecutionManagerAgent -> `docs/epics/00-epic_summary.md`
- `tasks` -> ExecutionManagerAgent -> `docs/epics/00-task-planning-across-all-epics.md` and `docs/epics/**`

Output:
1. Resolved comments
2. Remaining open comments
3. Proceed status
4. Updated files
