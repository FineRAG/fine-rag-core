---
description: "Run AnalyzerAgent to distill requirements with no assumptions and option-based clarification rounds."
---
Use `AnalyzerAgent` as subagent.

Inputs:
- Raw requirement text
- Existing docs path (if any)
- Output path (default: latest versioned requirements doc)

Instructions for AnalyzerAgent:
- Ask clarification questions in option format only.
- Max 10 questions per round.
- No assumptions.
- Resolve conflicts explicitly before finalization.

Expected outputs:
1. Round summary:
- Confirmed decisions
- Open questions with options
- Conflicts detected
- Proposed next round
2. Final file written:
- `docs/distilled_requirements.vX.md` (latest version)
