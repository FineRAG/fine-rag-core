#!/usr/bin/env bash
set -euo pipefail

# Sync local changes to EC2, deploy compose stack, then run health checks.
# This is intended for DeploymentAgent handoff automation.

DEFAULT_USER_HOST="ubuntu@ec2-3-7-70-60.ap-south-1.compute.amazonaws.com"
DEFAULT_REMOTE_PATH="/home/ubuntu/projects/finerag"
DEFAULT_SSH_KEY_PATH="/Users/shafeeq/Documents/01-New-Job/Prep/ai-serv/lul-mul-tul.pem"
DEFAULT_COMPOSE_FILE="docker-compose.yml"

usage() {
  cat <<'USAGE'
Usage:
  scripts/deploy_sync_health.sh [EC2_USER_HOST] [REMOTE_PATH] [SSH_KEY_PATH] [COMPOSE_FILE]

Examples:
  scripts/deploy_sync_health.sh
  scripts/deploy_sync_health.sh ubuntu@ec2-3-7-70-60.ap-south-1.compute.amazonaws.com /home/ubuntu/projects/finerag /Users/shafeeq/Documents/01-New-Job/Prep/ai-serv/lul-mul-tul.pem docker-compose.yml

Environment:
  RSYNC_DELETE=true|false   Default: true
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

USER_HOST="${1:-$DEFAULT_USER_HOST}"
REMOTE_PATH="${2:-$DEFAULT_REMOTE_PATH}"
SSH_KEY_PATH="${3:-$DEFAULT_SSH_KEY_PATH}"
COMPOSE_FILE="${4:-$DEFAULT_COMPOSE_FILE}"
RSYNC_DELETE="${RSYNC_DELETE:-true}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing required command: $1" >&2
    exit 1
  }
}

require_cmd rsync
require_cmd ssh

if [[ -n "$SSH_KEY_PATH" && ! -f "$SSH_KEY_PATH" ]]; then
  echo "SSH key file not found: $SSH_KEY_PATH" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

SSH_OPTS=(-o StrictHostKeyChecking=accept-new)
if [[ -n "$SSH_KEY_PATH" ]]; then
  SSH_OPTS+=(-i "$SSH_KEY_PATH")
fi

RSYNC_OPTS=(-az)
if [[ "$RSYNC_DELETE" == "true" ]]; then
  RSYNC_OPTS+=(--delete)
fi

# Keep deployment payload focused and deterministic.
RSYNC_OPTS+=(
  --exclude=.git
  --exclude=.github
  --exclude=.DS_Store
  --exclude=.idea
  --exclude=.vscode
  --exclude=node_modules
)

if [[ -f "${ROOT_DIR}/.gitignore" ]]; then
  RSYNC_OPTS+=("--exclude-from=${ROOT_DIR}/.gitignore")
fi

echo "== Syncing workspace to ${USER_HOST}:${REMOTE_PATH} =="
rsync "${RSYNC_OPTS[@]}" -e "ssh ${SSH_OPTS[*]}" "${ROOT_DIR}/" "${USER_HOST}:${REMOTE_PATH}/"

# Sync sibling UI repos so docker compose build contexts (../fine-rag-*-ui) resolve on EC2
MONOREPO_ROOT="$(cd "${ROOT_DIR}/.." && pwd)"
REMOTE_PARENT="$(dirname "${REMOTE_PATH}")"

for UI_REPO in fine-rag-ingestion-ui fine-rag-query-ui; do
  LOCAL_UI="${MONOREPO_ROOT}/${UI_REPO}"
  if [[ -d "${LOCAL_UI}" ]]; then
    echo "== Syncing UI repo ${UI_REPO} to ${USER_HOST}:${REMOTE_PARENT}/${UI_REPO} =="
    rsync "${RSYNC_OPTS[@]}" -e "ssh ${SSH_OPTS[*]}" "${LOCAL_UI}/" "${USER_HOST}:${REMOTE_PARENT}/${UI_REPO}/"
  fi
done

# Sync secrets directory explicitly (bypassing .gitignore)
if [[ -d "${ROOT_DIR}/secrets" ]]; then
  echo "== Syncing secrets to ${USER_HOST}:${REMOTE_PATH}/secrets =="
  # Ensure remote secrets directory exists
  ssh "${SSH_OPTS[@]}" "$USER_HOST" "mkdir -p '${REMOTE_PATH}/secrets'"
  # Sync secrets (no --delete to avoid nuking remote-only secrets if any)
  rsync -az -e "ssh ${SSH_OPTS[*]}" "${ROOT_DIR}/secrets/" "${USER_HOST}:${REMOTE_PATH}/secrets/"
fi

echo "== Running remote docker compose deployment =="
ssh "${SSH_OPTS[@]}" "$USER_HOST" "bash -s" <<REMOTE
set -e
set -u
set -o pipefail

export REMOTE_PATH='$REMOTE_PATH'
export COMPOSE_FILE='$COMPOSE_FILE'

cd "\$REMOTE_PATH"
docker compose -f "\$COMPOSE_FILE" pull
docker compose -f "\$COMPOSE_FILE" build
docker compose -f "\$COMPOSE_FILE" up -d --remove-orphans

if [[ -x "scripts/migration_bootstrap.sh" ]]; then
  echo "== Running migration bootstrap precheck =="
  ./scripts/migration_bootstrap.sh
else
  echo "== Migration bootstrap script not found/executable; skipping =="
fi

echo "== Compose status =="
docker compose -f "\$COMPOSE_FILE" ps
REMOTE

echo "== Running stack health checks =="
"${SCRIPT_DIR}/check_stack.sh" "$USER_HOST" "$REMOTE_PATH" "$SSH_KEY_PATH" "$COMPOSE_FILE"

echo "Deployment + health check completed successfully."
