# Workflow Control Panel

Use this file as the single checkpoint board for the workflow:
`requirements -> design -> epic -> task packs -> execution`

## Proceed Toggles
- [ ] Requirements approved (`.ai/reviews/requirements.review.md`)
- [ ] Design approved (`.ai/reviews/design.review.md`)
- [ ] Epic plan approved (`.ai/reviews/epic.review.md`)
- [ ] Task packs approved (`.ai/reviews/tasks.review.md`)
- [ ] Execution approved for Coding/Testing/Security/Deployment agents

## Active Stage
- Current stage: requirements
- Requested by:
- Date:

## Reviewer Notes
- Keep concise notes here, and detailed comments in stage-specific review files.

## Re-trigger Commands (natural language)
1. "Run AnalyzerAgent and resolve open comments in .ai/reviews/requirements.review.md"
2. "Run ArchitectAgent and resolve open comments in .ai/reviews/design.review.md"
3. "Run ExecutionManagerAgent and revise docs/epics/00-epic_summary.md per .ai/reviews/epic.review.md"
4. "Run ExecutionManagerAgent and revise docs/epics/** per .ai/reviews/tasks.review.md"
5. "Run ExecutionManagerAgent for approved task IDs from docs/epics/00-task-planning-across-all-epics.md and orchestrate Coding/Testing/Security/Deployment"
