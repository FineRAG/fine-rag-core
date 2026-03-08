#!/usr/bin/env bash
set -euo pipefail

# Usage:
# ./scripts/start_stage_review.sh requirements docs/distilled_requirements.md design
# ./scripts/start_stage_review.sh design docs/system_design.md epic

if [[ $# -lt 3 ]]; then
  echo "Usage: $0 <stage> <artifact_csv> <next_stage>"
  echo "Example: $0 requirements docs/distilled_requirements.md design"
  exit 1
fi

stage="$1"
artifacts_csv="$2"
next_stage="$3"

branch="review/${stage}-$(date +%Y%m%d-%H%M%S)"

git checkout -b "$branch"

IFS=',' read -r -a artifacts <<< "$artifacts_csv"
for file in "${artifacts[@]}"; do
  if [[ ! -f "$file" ]]; then
    echo "Missing artifact: $file"
    exit 1
  fi
done

git add "${artifacts[@]}"

git commit -m "review(${stage}): submit stage artifacts for line comments"

echo "Branch created: $branch"
echo "Next steps:"
echo "1) git push -u origin $branch"
echo "2) Create PR using template: stage-review.md"
echo "3) In PR, collect line comments and approvals"
echo "4) Add label proceed:${next_stage} only after approval"
