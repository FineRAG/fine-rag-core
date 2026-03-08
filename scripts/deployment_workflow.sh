#!/usr/bin/env bash
set -euo pipefail

# Deployment/versioning workflow helper for task-based delivery.
# Supports: branch creation, push, merge to dev, PR creation, rsync deploy, and docker log tailing.

usage() {
  cat <<'USAGE'
Usage:
  scripts/deployment_workflow.sh create-branch <TASK_ID> <WORD1> <WORD2>
  scripts/deployment_workflow.sh push <COMMIT_MESSAGE> [pathspec...]
  scripts/deployment_workflow.sh merge-to-dev <FEATURE_BRANCH>
  scripts/deployment_workflow.sh create-pr <FEATURE_BRANCH> [TITLE] [BODY]
  scripts/deployment_workflow.sh deploy-ec2 <EC2_USER_HOST> <REMOTE_PATH> [COMPOSE_FILE]
  scripts/deployment_workflow.sh all <TASK_ID> <WORD1> <WORD2> <COMMIT_MESSAGE> <EC2_USER_HOST> <REMOTE_PATH> [COMPOSE_FILE]

Environment variables:
  BASE_BRANCH=dev                    Base branch for feature branch creation and PR target.
  AUTO_RESOLVE_CONFLICTS=true        If true, retries merge with -X theirs when conflicts occur.
  EXCLUDE_FILE=.gitignore            Rsync exclude file.

Examples:
  scripts/deployment_workflow.sh create-branch E1-T1 core contracts
  scripts/deployment_workflow.sh push "feat(E1-T1): core contracts"
  scripts/deployment_workflow.sh merge-to-dev E1-T1-core-contracts
  scripts/deployment_workflow.sh create-pr E1-T1-core-contracts "feat(E1-T1): core contracts" "Implements task E1-T1"
  scripts/deployment_workflow.sh deploy-ec2 ec2-user@1.2.3.4 /opt/enterprise-go-rag
USAGE
}

BASE_BRANCH="${BASE_BRANCH:-dev}"
AUTO_RESOLVE_CONFLICTS="${AUTO_RESOLVE_CONFLICTS:-true}"
EXCLUDE_FILE="${EXCLUDE_FILE:-.gitignore}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing required command: $1" >&2
    exit 1
  }
}

require_git_repo() {
  git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
    echo "Not inside a git repository." >&2
    exit 1
  }
}

current_branch() {
  git rev-parse --abbrev-ref HEAD
}

create_branch() {
  local task_id="$1"
  local word1="$2"
  local word2="$3"

  require_git_repo
  local desc
  desc="${word1,,}-${word2,,}"
  desc="${desc//[^a-z0-9-]/}"
  local feature_branch="${task_id}-${desc}"

  git fetch origin "$BASE_BRANCH" || true
  git checkout "$BASE_BRANCH"
  git pull --ff-only origin "$BASE_BRANCH" || true
  git checkout -b "$feature_branch"

  echo "Created and switched to feature branch: $feature_branch"
}

push_changes() {
  local message="$1"
  shift || true

  require_git_repo
  local branch
  branch="$(current_branch)"

  if [[ "$#" -gt 0 ]]; then
    git add "$@"
  else
    git add -A
  fi

  if git diff --cached --quiet; then
    echo "No staged changes to commit on branch $branch"
  else
    git commit -m "$message"
  fi

  git push -u origin "$branch"
  echo "Pushed branch: $branch"
}

merge_to_dev() {
  local feature_branch="$1"

  require_git_repo
  git fetch origin
  git checkout "$BASE_BRANCH"
  git pull --ff-only origin "$BASE_BRANCH" || true

  if git merge --no-ff "$feature_branch" -m "merge(${feature_branch}): into ${BASE_BRANCH}"; then
    echo "Merged $feature_branch into $BASE_BRANCH"
  else
    echo "Merge conflict detected while merging $feature_branch into $BASE_BRANCH" >&2
    if [[ "$AUTO_RESOLVE_CONFLICTS" == "true" ]]; then
      echo "Attempting auto-resolve with strategy option -X theirs"
      git merge --abort || true
      if git merge --no-ff -X theirs "$feature_branch" -m "merge(${feature_branch}): auto-resolve into ${BASE_BRANCH}"; then
        echo "Auto-resolved merge conflicts using -X theirs"
      else
        echo "Auto-resolve failed. Please resolve conflicts manually:" >&2
        git diff --name-only --diff-filter=U || true
        exit 1
      fi
    else
      echo "AUTO_RESOLVE_CONFLICTS=false, manual conflict resolution required." >&2
      git diff --name-only --diff-filter=U || true
      exit 1
    fi
  fi

  git push origin "$BASE_BRANCH"
  echo "Pushed merged $BASE_BRANCH to origin"
}

create_pr() {
  local feature_branch="$1"
  local title="${2:-feat(${feature_branch}): merge into ${BASE_BRANCH}}"
  local body="${3:-Automated PR for ${feature_branch}}"

  require_git_repo
  if ! command -v gh >/dev/null 2>&1; then
    echo "GitHub CLI (gh) not found. Create PR manually:" >&2
    echo "Base: $BASE_BRANCH, Head: $feature_branch" >&2
    return 0
  fi

  gh pr create --base "$BASE_BRANCH" --head "$feature_branch" --title "$title" --body "$body"
}

deploy_ec2() {
  local user_host="$1"
  local remote_path="$2"
  local compose_file="${3:-docker-compose.yml}"

  require_cmd rsync
  require_cmd ssh

  local rsync_excludes=()
  if [[ -f "$EXCLUDE_FILE" ]]; then
    rsync_excludes+=("--exclude-from=$EXCLUDE_FILE")
  fi
  rsync_excludes+=("--exclude=.git" "--exclude=.github" "--exclude=tools")

  echo "Rsyncing project to ${user_host}:${remote_path}"
  rsync -az --delete "${rsync_excludes[@]}" ./ "${user_host}:${remote_path}/"

  echo "Running remote docker compose deployment"
  ssh "$user_host" "set -euo pipefail; cd '$remote_path'; docker compose -f '$compose_file' pull; docker compose -f '$compose_file' build; docker compose -f '$compose_file' up -d"

  echo "Tailing deployment logs (Ctrl+C to stop)"
  ssh "$user_host" "set -euo pipefail; cd '$remote_path'; docker compose -f '$compose_file' logs -f --tail=200"
}

all_flow() {
  local task_id="$1"
  local word1="$2"
  local word2="$3"
  local commit_message="$4"
  local user_host="$5"
  local remote_path="$6"
  local compose_file="${7:-docker-compose.yml}"

  create_branch "$task_id" "$word1" "$word2"
  push_changes "$commit_message"

  local feature_branch
  feature_branch="$(current_branch)"

  deploy_ec2 "$user_host" "$remote_path" "$compose_file"
  create_pr "$feature_branch" "feat(${task_id}): ${word1} ${word2}" "Task ${task_id} implementation and EC2 deployment"
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
      [[ $# -eq 3 ]] || { usage; exit 1; }
      create_branch "$1" "$2" "$3"
      ;;
    push)
      [[ $# -ge 1 ]] || { usage; exit 1; }
      push_changes "$@"
      ;;
    merge-to-dev)
      [[ $# -eq 1 ]] || { usage; exit 1; }
      merge_to_dev "$1"
      ;;
    create-pr)
      [[ $# -ge 1 ]] || { usage; exit 1; }
      create_pr "$@"
      ;;
    deploy-ec2)
      [[ $# -ge 2 ]] || { usage; exit 1; }
      deploy_ec2 "$@"
      ;;
    all)
      [[ $# -ge 6 ]] || { usage; exit 1; }
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
