#!/usr/bin/env bash
set -euo pipefail

# Database migration/bootstrap guard for deployment workflows.
# Fails fast on missing required external PostgreSQL URL when provider is postgres.

PROVIDER="${FINE_RAG_DB_PROVIDER:-memory}"
DB_URL="${FINE_RAG_DATABASE_URL:-}"
MIGRATIONS_DIR="${FINE_RAG_MIGRATIONS_DIR:-migrations}"

if [[ "${PROVIDER}" != "postgres" ]]; then
  echo "Migration bootstrap skipped: FINE_RAG_DB_PROVIDER=${PROVIDER} (local fallback mode)."
  exit 0
fi

if [[ -z "${DB_URL}" ]]; then
  echo "Migration bootstrap failed: FINE_RAG_DATABASE_URL is required when FINE_RAG_DB_PROVIDER=postgres." >&2
  exit 1
fi

if [[ ! -d "${MIGRATIONS_DIR}" ]]; then
  echo "Migration bootstrap failed: migration directory not found at ${MIGRATIONS_DIR}." >&2
  exit 1
fi

echo "Migration bootstrap precheck passed for provider=postgres"
echo "Migration files:"
ls -1 "${MIGRATIONS_DIR}"/*.sql

if ! command -v go >/dev/null 2>&1; then
  echo "Migration bootstrap failed: go toolchain not found for migration validation." >&2
  exit 1
fi

go test ./... -run 'DatabaseURL|MigrationBootstrap' -count=1

echo "Migration bootstrap validation completed."
