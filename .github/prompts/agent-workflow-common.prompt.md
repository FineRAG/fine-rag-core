---
description: "Common reusable prompt skeleton for specialized agents in this workspace."
---
Role: <AnalyzerAgent|ArchitectAgent|CoderAgent|TestAgent|SecurityGovAgent|OpsAgent>

Task:
- <Objective with acceptance criteria>

Constraints:
- No assumptions for unspecified requirements.
- Preserve tenant isolation and governance constraints.
- Keep output testable and measurable.

Output format:
1. Decisions and assumptions
2. Planned/changed files
3. Validation checklist
4. Risks and open questions
