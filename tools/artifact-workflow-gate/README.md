# Artifact Workflow Gate

Global VS Code extension that adds a proceed gate for markdown artifact workflows.

Designed to pair with line-comment extensions such as `jjkorpershoek.textfile-comments`.

## What it does
- Shows a dynamic right-side status item: `Proceed (N)` where `N` is unresolved comments for the active markdown file.
- Adds `Proceed` and `Refresh Review Count` buttons to editor title for markdown files.
- Blocks proceeding when unresolved comments exist (configurable).
- Optionally auto-checks review gate files in `.ai/reviews/*.review.md` based on artifact type.
- When blocked by unresolved comments, imports inline comments into the stage review file and prepares a current-stage agent prompt in `.ai/reviews/agent-trigger.md`.

## Expected comment source
By default, this extension reads `comments.json` from workspace root, which matches `textfile-comments` default behavior.

It also supports `Markdown Docs & Comments` inline directives in markdown files:
- `:comment[...]{...}`
- `::comment[...]{...}`
- `:::comment{...}`

These are treated as unresolved comments unless directive attributes explicitly contain `status="resolved"` or `resolved="true"`.

Configurable via:
- `artifactWorkflow.commentsFilename`

## Stage mapping
- `distilled_requirements*.md` -> requirements gate
- `system_design*.md` -> design gate
- `epic_execution_plan*.md` -> epic gate
- files in `.ai/tasks/` or `*task_pack*` -> tasks gate

## Settings
- `artifactWorkflow.blockProceedWithUnresolved` (default: true)
- `artifactWorkflow.commentsFilename` (default: comments.json)
- `artifactWorkflow.autoUpdateGateFiles` (default: true)
- `artifactWorkflow.mode` (`manual`, `agent-decides`, `auto`)
- `artifactWorkflow.commentSource` (`auto`, `textfile-comments`, `markdown-docs`)
- `artifactWorkflow.autoInvokeCopilot` (default: false, best-effort chat open + prompt copy)

## Build and install locally (global extension install)
```bash
cd tools/artifact-workflow-gate
npm install
npm run build
npx @vscode/vsce package
code --install-extension artifact-workflow-gate-0.1.0.vsix
```

Once installed, the extension is available globally across all VS Code projects.

## Publish to Marketplace
1. Create a publisher in Visual Studio Marketplace.
2. Replace `publisher` in `package.json` with your publisher ID.
3. Login: `npx @vscode/vsce login <publisher-id>`
4. Publish: `npx @vscode/vsce publish`

## Notes
This extension provides proceed-gating and count display. Line-level comment UI (`+` hover) is provided by the comments extension.
