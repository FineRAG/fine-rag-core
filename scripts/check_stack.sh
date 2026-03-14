#!/usr/bin/env bash
set -euo pipefail

# Health check helper for EC2-deployed docker compose stack.
# Verifies:
# 1) Expected services are running.
# 2) Core endpoints/ports are reachable.

usage() {
  cat <<'USAGE'
Usage:
  scripts/check_stack.sh <EC2_USER_HOST> <REMOTE_PATH> [SSH_KEY_PATH] [COMPOSE_FILE]

Examples:
  scripts/check_stack.sh ubuntu@1.2.3.4 /home/ubuntu/projects/finerag /path/key.pem
  scripts/check_stack.sh ubuntu@1.2.3.4 /home/ubuntu/projects/finerag /path/key.pem docker-compose.yml
USAGE
}

if [[ $# -lt 2 ]]; then
  usage
  exit 1
fi

USER_HOST="$1"
REMOTE_PATH="$2"
SSH_KEY_PATH="${3:-}"
COMPOSE_FILE="${4:-docker-compose.yml}"

ssh_opts=(-o StrictHostKeyChecking=accept-new)
if [[ -n "$SSH_KEY_PATH" ]]; then
  ssh_opts+=(-i "$SSH_KEY_PATH")
fi

ssh "${ssh_opts[@]}" "$USER_HOST" "REMOTE_PATH='$REMOTE_PATH' COMPOSE_FILE='$COMPOSE_FILE' bash -s" <<'REMOTE'
set -euo pipefail

cd "$REMOTE_PATH"

echo "== Stack Service Status =="
expected_services="$(docker compose -f "$COMPOSE_FILE" config --services)"
running_services="$(docker compose -f "$COMPOSE_FILE" ps --services --filter status=running || true)"

echo "Expected services:"
echo "$expected_services"
echo "Running services:"
echo "$running_services"

failures=0

check_running_service() {
  local svc="$1"
  if grep -xq "$svc" <<<"$running_services"; then
    echo "[PASS] service running: $svc"
  else
    echo "[FAIL] service not running: $svc"
    failures=$((failures + 1))
  fi
}

while IFS= read -r svc; do
  [[ -z "$svc" ]] && continue
  check_running_service "$svc"
done <<< "$expected_services"

check_cmd() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    echo "[PASS] $name"
  else
    echo "[FAIL] $name"
    failures=$((failures + 1))
  fi
}

echo "== Endpoint and Dependency Health =="
check_cmd "Backend health endpoint" bash -lc 'for i in {1..15}; do curl -fsS http://localhost:18080/healthz >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1'
if grep -xq "fine-rag-ingestion-ui" <<<"$expected_services"; then
  check_cmd "Ingestion UI port open (14173)" bash -lc "</dev/tcp/127.0.0.1/14173"
fi
if grep -xq "fine-rag-query-ui" <<<"$expected_services"; then
  check_cmd "Search UI port open (14174)" bash -lc "</dev/tcp/127.0.0.1/14174"
fi
if grep -xq "milvus" <<<"$expected_services"; then
  check_cmd "Milvus gRPC port open (19530)" bash -lc "</dev/tcp/127.0.0.1/19530"
  check_cmd "Milvus metrics port open (19091)" bash -lc "</dev/tcp/127.0.0.1/19091"
else
  echo "[INFO] Skipping Milvus local port checks (service not present in compose stack)."
fi

echo "[INFO] Prometheus and Grafana are expected to run as managed services, not inside this compose stack."

echo "== Compose PS =="
docker compose -f "$COMPOSE_FILE" ps

if [[ "$failures" -gt 0 ]]; then
  echo "Health check completed with failures: $failures"
  exit 1
fi

echo "Health check completed successfully."
REMOTE
