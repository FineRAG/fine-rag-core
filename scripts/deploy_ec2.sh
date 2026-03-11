#!/usr/bin/env bash
set -euo pipefail

# Backward-compatible wrapper for system design contract.
# Usage:
#   ./scripts/deploy_ec2.sh <EC2_USER_HOST> <REMOTE_PATH> [COMPOSE_FILE]

if [[ $# -lt 2 ]]; then
  echo "Usage: $0 <EC2_USER_HOST> <REMOTE_PATH> [COMPOSE_FILE]"
  exit 1
fi

"$(dirname "$0")/deployment_workflow.sh" deploy-ec2 "$@"
