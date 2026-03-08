---
description: "Run ExecutionManagerAgent to orchestrate dependency-aware execution planning, task status tracking, and Coding/Testing/Security/Deployment handoffs under docs/epics/."
---
Use `ExecutionManagerAgent`.

Inputs:
- `docs/epics/00-task-planning-across-all-epics.md`
- `docs/epics/**/00-task-planning-*-epic.md`
- `docs/epics/**/*-Task.md`
- `.ai/reviews/tasks.review.md`

Rules:
- Produce only latest non-versioned files.
- Update:
  - `docs/epics/00-execution-plan.md`
  - `docs/epics/00-task-execution-status.md`
  - `docs/epics/00-deployment-status.md`
- Keep dependency order and per-task status current.
- Coordinate CodingAgent, TestingAgent, SecurityGovAgent, and DeploymentAgent feedback loops.
- Ask user approval before progressing to the next task.

Output:
1. Updated execution plan and status files
2. Next task selected with dependency validation
3. Handoff payloads and feedback routing plan for Coding/Testing/Security/Deployment agents
