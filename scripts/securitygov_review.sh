#!/usr/bin/env bash
set -euo pipefail

# SecurityGov review runner for repeatable local governance checks.
# Runs baseline tests plus optional security tools when installed.

usage() {
  cat <<'USAGE'
Usage:
  scripts/securitygov_review.sh [GO_TEST_PATTERN]

Examples:
  scripts/securitygov_review.sh
  scripts/securitygov_review.sh 'Auth|APIKey|RateLimit|Profiler|MetadataSchema'
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

PATTERN="${1:-Auth|APIKey|RateLimit|Quota|IngestionProfile|Profiler|MetadataSchema}"

run_optional() {
  local tool_name="$1"
  shift
  if command -v "$tool_name" >/dev/null 2>&1; then
    echo "== Running $tool_name =="
    "$@"
  else
    echo "== Skipping $tool_name (not installed) =="
  fi
}

echo "== SecurityGov baseline tests =="
go test ./... -run "$PATTERN" -count=1

echo "== SecurityGov optional static checks =="
run_optional gosec gosec ./...
run_optional govulncheck govulncheck ./...

# Keep this optional to avoid blocking environments that do not use golangci-lint.
if command -v golangci-lint >/dev/null 2>&1; then
  echo "== Running golangci-lint =="
  golangci-lint run
else
  echo "== Skipping golangci-lint (not installed) =="
fi

echo "SecurityGov review completed."
