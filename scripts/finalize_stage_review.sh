#!/usr/bin/env bash
set -euo pipefail

# This script records review outcome in local gate files after PR approval.
# Usage:
# ./scripts/finalize_stage_review.sh requirements approved
# ./scripts/finalize_stage_review.sh design changes-requested

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <stage> <approved|changes-requested>"
  exit 1
fi

stage="$1"
outcome="$2"

case "$stage" in
  requirements)
    gate_file=".ai/reviews/requirements.review.md"
    proceed_line="- [ ] Proceed to Design"
    proceed_marked="- [x] Proceed to Design"
    ;;
  design)
    gate_file=".ai/reviews/design.review.md"
    proceed_line="- [ ] Proceed to Epic Planning"
    proceed_marked="- [x] Proceed to Epic Planning"
    ;;
  epic)
    gate_file=".ai/reviews/epic.review.md"
    proceed_line="- [ ] Proceed to Task Packs"
    proceed_marked="- [x] Proceed to Task Packs"
    ;;
  tasks)
    gate_file=".ai/reviews/tasks.review.md"
    proceed_line="- [ ] Proceed to Coding/Testing/Security Execution"
    proceed_marked="- [x] Proceed to Coding/Testing/Security Execution"
    ;;
  *)
    echo "Unsupported stage: $stage"
    exit 1
    ;;
esac

if [[ ! -f "$gate_file" ]]; then
  echo "Gate file not found: $gate_file"
  exit 1
fi

if [[ "$outcome" == "approved" ]]; then
  if command -v perl >/dev/null 2>&1; then
    perl -0777 -i -pe "s/\Q$proceed_line\E/$proceed_marked/g" "$gate_file"
  else
    sed -i '' "s|$proceed_line|$proceed_marked|g" "$gate_file"
  fi
  echo "Marked proceed gate approved in $gate_file"
else
  echo "Review outcome indicates changes requested. Proceed gate remains unchecked in $gate_file"
fi
