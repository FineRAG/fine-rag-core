---
description: "Run a stage in PR-review mode: generate artifact, submit for line comments, and wait for proceed gate approval."
---
Mode: PR-review-gated

Inputs:
- Stage: requirements | design | epic | tasks
- Artifact paths
- Next stage

Rules:
- Generate or revise only current stage artifact(s).
- Do not advance to next stage until review approval is confirmed.
- Instruct user to run `scripts/start_stage_review.sh` for PR line comments.
- After approval, update gate via `scripts/finalize_stage_review.sh`.
- Then and only then proceed to the next agent stage.

Output:
1. Updated artifact files
2. PR-review command to run
3. Gate status
4. Next allowed stage
