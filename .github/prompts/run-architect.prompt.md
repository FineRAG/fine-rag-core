---
description: "Run ArchitectAgent to convert distilled requirements into a cost-efficient, infra-efficient system design."
---
Use `ArchitectAgent` as subagent.

Inputs:
- Distilled requirements path (required)
- Design output path (default: latest versioned system design doc)

Instructions for ArchitectAgent:
- Produce architecture directly from requirements.
- Include cost-saving and infra-saving recommendations with trade-offs.
- Include requirement-to-design traceability.
- Flag unresolved risks and open decisions.

Expected outputs:
1. Design summary
2. Traceability table
3. Optimization recommendations
4. Risks/open issues
5. Final file written: `docs/system_design.vX.md`
