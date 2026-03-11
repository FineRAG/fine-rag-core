#!/usr/bin/env bash
set -euo pipefail

# Branch/create/push/PR/merge helper for repetitive task delivery.
# This is intended for DeploymentAgent branch/versioning handoff automation.

usage() {
  cat <<'USAGE'
Usage:
  scripts/git_task_flow.sh create-branch <TASK_ID> <SLUG>
  scripts/git_task_flow.sh commit-push <COMMIT_MESSAGE> [pathspec...]
  scripts/git_task_flow.sh create-pr <FEATURE_BRANCH> [TITLE] [BODY]
  scripts/git_task_flow.sh merge-dev <FEATURE_BRANCH>
  scripts/git_task_flow.sh all <TASK_ID> <SLUG> <COMMIT_MESSAGE> [PR_TITLE] [PR_BODY]

Examples:
  scripts/git_task_flow.sh create-branch E1-T3 auth-rate-limit
  scripts/git_task_flow.sh commit-push "feat(E1-T3): auth and rate limit"
  scripts/git_task_flow.sh create-pr feature/E1-T3-auth-rate-limit
  scripts/git_task_flow.sh merge-dev feature/E1-T3-auth-rate-limit
  scripts/git_task_flow.sh all E2-T1 ingestion-profiler "feat(E2-T1): ingestion profiling"

Environment:
  BASE_BRANCH=dev            Base branch for feature branch and merge target.
  AUTO_PUSH_BASE=true|false  Push base branch after merge (default true).
USAGE
}

BASE_BRANCH="${BASE_BRANCH:-dev}"
AUTO_PUSH_BASE="${AUTO_PUSH_BASE:-true}"

require_git_repo() {
  git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
    echo "Not inside a git repository." >&2
    exit 1
  }
}

current_branch() {
  git rev-parse --abbrev-ref HEAD
}

sanitize_slug() {
  local raw="$1"
  local out
  out="$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')"
  out="${out// /-}"
  out="${out//[^a-z0-9-]/}"
  out="${out##-}"
  out="${out%%-}"
  echo "$out"
}

feature_branch_name() {
  local task_id="$1"
  local slug="$2"
  echo "feature/${task_id}-$(sanitize_slug "$slug")"
}

create_branch() {
  local task_id="$1"
  local slug="$2"
  local branch
  branch="$(feature_branch_name "$task_id" "$slug")"

  require_git_repo
  git fetch origin "$BASE_BRANCH"
  git checkout "$BASE_BRANCH"
  git pull --ff-only origin "$BASE_BRANCH"

  if git show-ref --verify --quiet "refs/heads/$branch"; then
    git checkout "$branch"
  else
    git checkout -b "$branch"
  fi

  echo "$branch"
}

commit_push() {
  local message="$1"
  shift || true

  require_git_repo

  if [[ "$#" -gt 0 ]]; then
    git add "$@"
  else
    git add -A
  fi

  if git diff --cached --quiet; then
    echo "No staged changes to commit."
  else
    git commit -m "$message"
  fi

  local branch
  branch="$(current_branch)"
  git push -u origin "$branch"
}

create_pr() {
  local feature_branch="$1"
  local title="${2:-feat(${feature_branch}): merge to ${BASE_BRANCH}}"
  local body="${3:-Automated PR created by git_task_flow.sh}"

  require_git_repo

  if command -v gh >/dev/null 2>&1; then
    gh pr create --base "$BASE_BRANCH" --head "$feature_branch" --title "$title" --body "$body"
  else
    echo "gh CLI is not installed. Create PR manually:"
    echo "  base: $BASE_BRANCH"
    echo "  head: $feature_branch"
  fi
}

merge_dev() {
  local feature_branch="$1"

  require_git_repo

  git fetch origin
  git checkout "$BASE_BRANCH"
  git pull --ff-only origin "$BASE_BRANCH"
  git merge --no-ff "$feature_branch" -m "merge(${feature_branch}): into ${BASE_BRANCH}"

  if [[ "$AUTO_PUSH_BASE" == "true" ]]; then
    git push origin "$BASE_BRANCH"
  fi
}

all_flow() {
  local task_id="$1"
  local slug="$2"
  local commit_message="$3"
  local pr_title="${4:-feat(${task_id}): ${slug}}"
  local pr_body="${5:-Automated task flow for ${task_id}}"

  local branch
  branch="$(create_branch "$task_id" "$slug")"
  commit_push "$commit_message"
  create_pr "$branch" "$pr_title" "$pr_body"
  merge_dev "$branch"

  echo "Completed flow for ${task_id} on ${branch}"
}

main() {
  if [[ $# -lt 1 ]]; then
    usage
    exit 1
  fi

  local cmd="$1"
  shift

  case "$cmd" in
    create-branch)
      [[ $# -eq 2 ]] || { usage; exit 1; }
      create_branch "$1" "$2"
      ;;
    commit-push)
      [[ $# -ge 1 ]] || { usage; exit 1; }
      commit_push "$@"
      ;;
    create-pr)
      [[ $# -ge 1 ]] || { usage; exit 1; }
      create_pr "$@"
      ;;
    merge-dev)
      [[ $# -eq 1 ]] || { usage; exit 1; }
      merge_dev "$1"
      ;;
    all)
      [[ $# -ge 3 ]] || { usage; exit 1; }
      all_flow "$@"
      ;;
    -h|--help|help)
      usage
      ;;
    *)
      echo "Unknown command: $cmd" >&2
      usage
      exit 1
      ;;
  esac
}

main "$@"
