#!/usr/bin/env bash
set -euo pipefail

# One-command helper:
# 1) Optional deploy/sync to EC2 using scripts/deploy_sync_health.sh
# 2) Open SSH tunnels so remote UI services are accessible on localhost

DEFAULT_USER_HOST="ubuntu@ec2-3-7-70-60.ap-south-1.compute.amazonaws.com"
DEFAULT_REMOTE_PATH="/home/ubuntu/projects/finerag"
DEFAULT_SSH_KEY_PATH="/Users/shafeeq/Documents/01-New-Job/Prep/ai-serv/lul-mul-tul.pem"
DEFAULT_COMPOSE_FILE="docker-compose.yml"

DEFAULT_LOCAL_INGESTION_PORT="14173"
DEFAULT_LOCAL_SEARCH_PORT="14174"
DEFAULT_REMOTE_INGESTION_PORT="14173"
DEFAULT_REMOTE_SEARCH_PORT="14174"

USER_HOST="$DEFAULT_USER_HOST"
REMOTE_PATH="$DEFAULT_REMOTE_PATH"
SSH_KEY_PATH="$DEFAULT_SSH_KEY_PATH"
COMPOSE_FILE="$DEFAULT_COMPOSE_FILE"

LOCAL_INGESTION_PORT="$DEFAULT_LOCAL_INGESTION_PORT"
LOCAL_SEARCH_PORT="$DEFAULT_LOCAL_SEARCH_PORT"
REMOTE_INGESTION_PORT="$DEFAULT_REMOTE_INGESTION_PORT"
REMOTE_SEARCH_PORT="$DEFAULT_REMOTE_SEARCH_PORT"

SKIP_DEPLOY="false"

usage() {
  cat <<'USAGE'
Usage:
  ./run_stack_with_tunnel.sh [options]

Options:
  --skip-deploy                      Skip deploy step and open tunnel only
  --user-host <user@host>            EC2 SSH target
  --remote-path <path>               Remote workspace path
  --ssh-key <path>                   SSH private key path
  --compose-file <file>              Compose file name on remote host
  --local-ingestion-port <port>      Local tunnel port for ingestion dashboard (default 14173)
  --local-search-port <port>         Local tunnel port for search query UI (default 14174)
  --remote-ingestion-port <port>     Remote ingestion dashboard port (default 14173)
  --remote-search-port <port>        Remote search query UI port (default 14174)
  -h, --help                         Show help

Examples:
  ./run_stack_with_tunnel.sh
  ./run_stack_with_tunnel.sh --skip-deploy
  ./run_stack_with_tunnel.sh --user-host ubuntu@1.2.3.4 --ssh-key ~/.ssh/finerag.pem
USAGE
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing required command: $1" >&2
    exit 1
  }
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --skip-deploy)
        SKIP_DEPLOY="true"
        shift
        ;;
      --user-host)
        USER_HOST="$2"
        shift 2
        ;;
      --remote-path)
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
      --local-ingestion-port)
        LOCAL_INGESTION_PORT="$2"
        shift 2
        ;;
      --local-search-port)
        LOCAL_SEARCH_PORT="$2"
        shift 2
        ;;
      --remote-ingestion-port)
        REMOTE_INGESTION_PORT="$2"
        shift 2
        ;;
      --remote-search-port)
        REMOTE_SEARCH_PORT="$2"
        shift 2
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
  parse_args "$@"

  require_cmd ssh

  if [[ -n "$SSH_KEY_PATH" && ! -f "$SSH_KEY_PATH" ]]; then
    echo "SSH key file not found: $SSH_KEY_PATH" >&2
    exit 1
  fi

  if [[ "$SKIP_DEPLOY" != "true" ]]; then
    echo "== Deploying stack to ${USER_HOST}:${REMOTE_PATH} =="
    ./scripts/deploy_sync_health.sh "$USER_HOST" "$REMOTE_PATH" "$SSH_KEY_PATH" "$COMPOSE_FILE"
  else
    echo "== Skipping deploy; opening tunnel only =="
  fi

  SSH_OPTS=(-o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -o ServerAliveCountMax=3)
  if [[ -n "$SSH_KEY_PATH" ]]; then
    SSH_OPTS+=(-i "$SSH_KEY_PATH")
  fi

  echo "== Opening UI tunnels =="
  echo "Ingestion Dashboard: http://localhost:${LOCAL_INGESTION_PORT}"
  echo "Search Query UI:     http://localhost:${LOCAL_SEARCH_PORT}"
  echo "Press Ctrl+C to close tunnels."

  exec ssh "${SSH_OPTS[@]}" \
    -L "${LOCAL_INGESTION_PORT}:127.0.0.1:${REMOTE_INGESTION_PORT}" \
    -L "${LOCAL_SEARCH_PORT}:127.0.0.1:${REMOTE_SEARCH_PORT}" \
    "$USER_HOST" -N
}

main "$@"
