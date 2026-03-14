#!/usr/bin/env bash
set -euo pipefail

# Top-level orchestrator for task delivery lifecycle:
# pick task -> security review -> branch/commit/push -> deploy -> health check -> PR -> merge.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
TASK_BOARD="${ROOT_DIR}/docs/epics/00-task-planning-across-all-epics.md"

GIT_FLOW_SCRIPT="${SCRIPT_DIR}/git_task_flow.sh"
DEPLOY_SCRIPT="${SCRIPT_DIR}/deploy_sync_health.sh"
SECURITY_SCRIPT="${SCRIPT_DIR}/securitygov_review.sh"

TASK_ID=""
TASK_TITLE=""
SLUG=""
COMMIT_MESSAGE=""
PR_TITLE=""
PR_BODY=""
TEST_PATTERN=""
EC2_USER_HOST=""
REMOTE_PATH=""
SSH_KEY_PATH=""
COMPOSE_FILE="docker-compose.yml"

SKIP_SECURITY="false"
SKIP_DEPLOY="false"
SKIP_PR="false"
SKIP_MERGE="false"
AUTO_PICK="false"

PATHS_TO_COMMIT=()

usage() {
  cat <<'USAGE'
Usage:
  scripts/task_orchestrate.sh [options]

Options:
  --task <TASK_ID>                 Example: E1-T3
  --slug <slug>                    Example: auth-rate-limit
  --commit <message>               Commit message for task changes
  --path <pathspec>                Optional repeated pathspec(s) to stage
  --ec2 <EC2_USER_HOST>            Example: ubuntu@3.27.153.189
  --remote <REMOTE_PATH>           Example: /home/ubuntu/projects/finerag
  --ssh-key <SSH_KEY_PATH>         Example: ~/.ssh/finerag.pem
  --compose-file <file>            Default: docker-compose.yml
  --pr-title <title>               Optional PR title
  --pr-body <body>                 Optional PR body
  --test-pattern <regex>           Optional SecurityGov go test pattern
  --pick                           Pick first open task from task board
  --skip-security                  Skip SecurityGov review step
  --skip-deploy                    Skip EC2 deploy + health-check step
  --skip-pr                        Skip PR creation step
  --skip-merge                     Skip merge to dev step
  -h, --help                       Show help

Examples:
  scripts/task_orchestrate.sh --pick --slug auth-rate-limit \
    --commit "feat(E1-T3): auth and rate limit" \
    --ec2 ubuntu@3.27.153.189 --remote /home/ubuntu/projects/finerag --ssh-key ~/.ssh/finerag.pem

  scripts/task_orchestrate.sh --task E2-T1 --slug ingestion-profiler \
    --commit "feat(E2-T1): ingestion profiling" \
    --ec2 ubuntu@3.27.153.189 --remote /home/ubuntu/projects/finerag --ssh-key ~/.ssh/finerag.pem \
    --test-pattern 'IngestionProfile|Profiler|MetadataSchema'
USAGE
}

require_file() {
  local p="$1"
  [[ -f "$p" ]] || {
    echo "Required file not found: $p" >&2
    exit 1
  }
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

default_test_pattern_for_task() {
  local id="$1"
  case "$id" in
    E1-T3)
      echo "Auth|APIKey|RateLimit|Quota"
      ;;
    E2-T1)
      echo "IngestionProfile|Profiler|MetadataSchema"
      ;;
    *)
      echo "Contract|Architecture|Isolation|Auth|APIKey|RateLimit|Quota|IngestionProfile|Profiler|MetadataSchema"
      ;;
  esac
}

find_open_tasks() {
  require_file "$TASK_BOARD"
  sed -nE 's/^- \[ \] `([^`]+)` (.*)$/\1|\2/p' "$TASK_BOARD"
}

pick_first_open_task() {
  local first
  first="$(find_open_tasks | head -n 1 || true)"
  if [[ -z "$first" ]]; then
    echo "No open tasks found in ${TASK_BOARD}" >&2
    exit 1
  fi

  TASK_ID="${first%%|*}"
  TASK_TITLE="${first#*|}"
}

derive_defaults() {
  if [[ -z "$SLUG" ]]; then
    if [[ -n "$TASK_TITLE" ]]; then
      SLUG="$(sanitize_slug "$TASK_TITLE")"
    else
      SLUG="$(sanitize_slug "$TASK_ID")"
    fi
  else
    SLUG="$(sanitize_slug "$SLUG")"
  fi

  if [[ -z "$COMMIT_MESSAGE" ]]; then
    COMMIT_MESSAGE="feat(${TASK_ID}): task delivery via orchestrator"
  fi

  if [[ -z "$PR_TITLE" ]]; then
    PR_TITLE="feat(${TASK_ID}): ${SLUG}"
  fi

  if [[ -z "$PR_BODY" ]]; then
    PR_BODY="Automated task delivery for ${TASK_ID} using task_orchestrate.sh"
  fi

  if [[ -z "$TEST_PATTERN" ]]; then
    TEST_PATTERN="$(default_test_pattern_for_task "$TASK_ID")"
  fi
}

feature_branch_name() {
  echo "feature/${TASK_ID}-${SLUG}"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --task)
        TASK_ID="$2"
        shift 2
        ;;
      --slug)
        SLUG="$2"
        shift 2
        ;;
      --commit)
        COMMIT_MESSAGE="$2"
        shift 2
        ;;
      --path)
        PATHS_TO_COMMIT+=("$2")
        shift 2
        ;;
      --ec2)
        EC2_USER_HOST="$2"
        shift 2
        ;;
      --remote)
        REMOTE_PATH="$2"
        shift 2
        ;;
      --ssh-key)
        SSH_KEY_PATH="$2"
        shift 2
        ;;
      --compose-file)
        COMPOSE_FILE="$2"
        shift 2
        ;;
      --pr-title)
        PR_TITLE="$2"
        shift 2
        ;;
      --pr-body)
        PR_BODY="$2"
        shift 2
        ;;
      --test-pattern)
        TEST_PATTERN="$2"
        shift 2
        ;;
      --pick)
        AUTO_PICK="true"
        shift
        ;;
      --skip-security)
        SKIP_SECURITY="true"
        shift
        ;;
      --skip-deploy)
        SKIP_DEPLOY="true"
        shift
        ;;
      --skip-pr)
        SKIP_PR="true"
        shift
        ;;
      --skip-merge)
        SKIP_MERGE="true"
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "Unknown option: $1" >&2
        usage
        exit 1
        ;;
    esac
  done
}

main() {
  require_file "$GIT_FLOW_SCRIPT"
  require_file "$DEPLOY_SCRIPT"
  require_file "$SECURITY_SCRIPT"

  parse_args "$@"

  if [[ "$AUTO_PICK" == "true" && -z "$TASK_ID" ]]; then
    pick_first_open_task
  fi

  if [[ -z "$TASK_ID" ]]; then
    echo "Missing required --task <TASK_ID> (or use --pick)." >&2
    usage
    exit 1
  fi

  derive_defaults

  local branch
  branch="$(feature_branch_name)"

  echo "== Task Orchestrator =="
  echo "Task ID: ${TASK_ID}"
  echo "Branch: ${branch}"
  echo "Security pattern: ${TEST_PATTERN}"

  if [[ "$SKIP_SECURITY" != "true" ]]; then
    echo "== Step 1/6: SecurityGov review =="
    "$SECURITY_SCRIPT" "$TEST_PATTERN"
  else
    echo "== Step 1/6: SecurityGov review skipped =="
  fi

  echo "== Step 2/6: Create feature branch =="
  "$GIT_FLOW_SCRIPT" create-branch "$TASK_ID" "$SLUG"

  echo "== Step 3/6: Commit and push =="
  if [[ "${#PATHS_TO_COMMIT[@]}" -gt 0 ]]; then
    "$GIT_FLOW_SCRIPT" commit-push "$COMMIT_MESSAGE" "${PATHS_TO_COMMIT[@]}"
  else
    "$GIT_FLOW_SCRIPT" commit-push "$COMMIT_MESSAGE"
  fi

  if [[ "$SKIP_DEPLOY" != "true" ]]; then
    if [[ -z "$EC2_USER_HOST" || -z "$REMOTE_PATH" ]]; then
      echo "Deploy step requires --ec2 and --remote unless --skip-deploy is used." >&2
      exit 1
    fi

    echo "== Step 4/6: Deploy + health check =="
    "$DEPLOY_SCRIPT" "$EC2_USER_HOST" "$REMOTE_PATH" "$SSH_KEY_PATH" "$COMPOSE_FILE"
  else
    echo "== Step 4/6: Deploy + health check skipped =="
  fi

  if [[ "$SKIP_PR" != "true" ]]; then
    echo "== Step 5/6: Create PR =="
    "$GIT_FLOW_SCRIPT" create-pr "$branch" "$PR_TITLE" "$PR_BODY"
  else
    echo "== Step 5/6: Create PR skipped =="
  fi

  if [[ "$SKIP_MERGE" != "true" ]]; then
    echo "== Step 6/6: Merge feature to dev =="
    "$GIT_FLOW_SCRIPT" merge-dev "$branch"
  else
    echo "== Step 6/6: Merge skipped =="
  fi

  echo "Task orchestration completed for ${TASK_ID}."
}

main "$@"
