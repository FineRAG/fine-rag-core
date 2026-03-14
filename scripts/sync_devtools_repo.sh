#!/usr/bin/env bash
set -euo pipefail

# Sync development workflow artifacts to fine-rag-devtools and push.
# Default devtools repo: git@github.com:FineRAG/fine-rag-devtools.git

usage() {
  cat <<'USAGE'
Usage:
  scripts/sync_devtools_repo.sh [--target-dir <path>] [--branch <name>] [--message <commit_message>] [--init-local]

Options:
  --target-dir   Local path for devtools repo clone (default: ../fine-rag-devtools)
  --branch       Branch to commit/push in devtools repo (default: current branch)
  --message      Commit message (default: chore: sync dev workflow artifacts)
  --init-local   Init a local git repo instead of cloning from GitHub (use when remote not yet created)
  -h, --help     Show help

Examples:
  scripts/sync_devtools_repo.sh
  scripts/sync_devtools_repo.sh --init-local
  scripts/sync_devtools_repo.sh --message "chore(epic-1): sync dev artifacts"
  scripts/sync_devtools_repo.sh --target-dir /Users/shafeeq/Documents/projects/fine-rag-devtools --branch main
USAGE
}

REPO_URL="git@github.com:FineRAG/fine-rag-devtools.git"
TARGET_DIR="../fine-rag-devtools"
TARGET_BRANCH=""
COMMIT_MESSAGE="chore: sync dev workflow artifacts"
INIT_LOCAL=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target-dir)
      TARGET_DIR="$2"
      shift 2
      ;;
    --branch)
      TARGET_BRANCH="$2"
      shift 2
      ;;
    --message)
      COMMIT_MESSAGE="$2"
      shift 2
      ;;
    --init-local)
      INIT_LOCAL=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing required command: $1" >&2
    exit 1
  }
}

require_cmd git
require_cmd rsync

WORKSPACE_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET_DIR_ABS="$(cd "$(dirname "$TARGET_DIR")" && pwd)/$(basename "$TARGET_DIR")"

if [[ ! -d "$TARGET_DIR_ABS/.git" ]]; then
  if [[ "$INIT_LOCAL" == "true" ]]; then
    echo "Initialising local devtools repo at: $TARGET_DIR_ABS"
    mkdir -p "$TARGET_DIR_ABS"
    git -C "$TARGET_DIR_ABS" init
    git -C "$TARGET_DIR_ABS" checkout -b main 2>/dev/null || true
  else
    echo "Cloning devtools repo into: $TARGET_DIR_ABS"
    git clone "$REPO_URL" "$TARGET_DIR_ABS"
  fi
fi

pushd "$TARGET_DIR_ABS" >/dev/null
remote_url="$(git remote get-url origin 2>/dev/null || true)"
if [[ -n "$remote_url" ]]; then
  if [[ "$remote_url" != "$REPO_URL" ]]; then
    echo "Warning: target repo remote is '$remote_url' (expected '$REPO_URL')." >&2
  fi
  git fetch origin
fi

if [[ -n "$TARGET_BRANCH" ]]; then
  if git show-ref --verify --quiet "refs/heads/$TARGET_BRANCH"; then
    git checkout "$TARGET_BRANCH"
  else
    git checkout -b "$TARGET_BRANCH"
  fi
  git pull --ff-only origin "$TARGET_BRANCH" 2>/dev/null || true
else
  current_branch="$(git branch --show-current)"
  if [[ -z "$current_branch" ]]; then
    default_branch="$(git remote show origin 2>/dev/null | awk '/HEAD branch/ {print $NF}')"
    default_branch="${default_branch:-main}"
    if git show-ref --verify --quiet "refs/heads/$default_branch"; then
      git checkout "$default_branch"
    else
      git checkout -b "$default_branch"
    fi
    git pull --ff-only origin "$default_branch" 2>/dev/null || true
  fi
fi

mkdir -p .ai .github docs/architecture docs/epics scripts tools

sync_dir() {
  local src="$1"
  local dst="$2"
  if [[ -d "$src" ]]; then
    rsync -az --delete "$src/" "$dst/"
  else
    echo "Skipping missing source dir: $src"
  fi
}

sync_file() {
  local src="$1"
  local dst="$2"
  if [[ -f "$src" ]]; then
    mkdir -p "$(dirname "$dst")"
    rsync -az "$src" "$dst"
  else
    echo "Skipping missing source file: $src"
  fi
}

# Dev workflow directories
sync_dir "$WORKSPACE_ROOT/.ai"              "$TARGET_DIR_ABS/.ai"
sync_dir "$WORKSPACE_ROOT/.github"          "$TARGET_DIR_ABS/.github"
sync_dir "$WORKSPACE_ROOT/docs/epics"       "$TARGET_DIR_ABS/docs/epics"
sync_dir "$WORKSPACE_ROOT/docs/architecture" "$TARGET_DIR_ABS/docs/architecture"
sync_dir "$WORKSPACE_ROOT/scripts"          "$TARGET_DIR_ABS/scripts"
sync_dir "$WORKSPACE_ROOT/tools"            "$TARGET_DIR_ABS/tools"

# Standalone dev docs
sync_file "$WORKSPACE_ROOT/docs/workflow_review.md"       "$TARGET_DIR_ABS/docs/workflow_review.md"
sync_file "$WORKSPACE_ROOT/docs/workflow_review_guide.md" "$TARGET_DIR_ABS/docs/workflow_review_guide.md"

# Requirements and system design docs (dev/planning artifacts)
for f in "$WORKSPACE_ROOT"/docs/distilled_requirements*.md; do
  [[ -f "$f" ]] && sync_file "$f" "$TARGET_DIR_ABS/docs/$(basename "$f")"
done
for f in "$WORKSPACE_ROOT"/docs/system_design*.md; do
  [[ -f "$f" ]] && sync_file "$f" "$TARGET_DIR_ABS/docs/$(basename "$f")"
done

git add -A
if git diff --cached --quiet; then
  echo "No changes to commit in devtools repo."
else
  git commit -m "$COMMIT_MESSAGE"
  if git remote get-url origin >/dev/null 2>&1; then
    active_branch="$(git branch --show-current)"
    git push -u origin "$active_branch"
    echo "Pushed devtools updates to branch: $active_branch"
  else
    echo "No remote configured — committed locally only."
    echo "Add a remote with: git -C '$TARGET_DIR_ABS' remote add origin <url> && git push -u origin main"
  fi
fi

popd >/dev/null

echo "Sync complete."
