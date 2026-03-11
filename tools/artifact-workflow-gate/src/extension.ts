import * as fs from "node:fs";
import * as path from "node:path";
import * as vscode from "vscode";

type ReviewStage = "requirements" | "design" | "epic" | "tasks" | "unknown";

type StoredThread = {
  uri?: { fsPath?: string };
  contextValue?: string;
  comments?: Array<{ label?: string }>;
};

export function activate(context: vscode.ExtensionContext): void {
  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 1000);
  status.command = "artifactWorkflow.proceed";
  status.tooltip = "Proceed to next agent stage";
  context.subscriptions.push(status);

  const refresh = async () => {
    const activeUri = getActiveMarkdownUri();
    if (!activeUri) {
      status.hide();
      return;
    }

    const unresolved = getUnresolvedCountForUri(activeUri);
    status.text = unresolved > 0 ? `$(warning) Proceed (${unresolved})` : "$(check) Proceed";
    status.show();
  };

  context.subscriptions.push(
    vscode.window.onDidChangeActiveTextEditor(() => {
      void refresh();
    }),
    vscode.workspace.onDidChangeTextDocument(() => {
      void refresh();
    }),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("artifactWorkflow") || e.affectsConfiguration("textfile_comments")) {
        void refresh();
      }
    }),
    vscode.window.tabGroups.onDidChangeTabs(() => {
      void refresh();
    }),
    vscode.window.tabGroups.onDidChangeTabGroups(() => {
      void refresh();
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("artifactWorkflow.refresh", async () => {
      await refresh();
      vscode.window.showInformationMessage("Artifact review count refreshed.");
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("artifactWorkflow.openCommentsFile", async () => {
      const commentsFile = getCommentsFilePath();
      if (!commentsFile) {
        vscode.window.showWarningMessage("No workspace folder found.");
        return;
      }
      if (!fs.existsSync(commentsFile)) {
        vscode.window.showWarningMessage(`Comments file not found: ${commentsFile}`);
        return;
      }
      const doc = await vscode.workspace.openTextDocument(vscode.Uri.file(commentsFile));
      await vscode.window.showTextDocument(doc, { preview: false });
    })
  );

  context.subscriptions.push(
    vscode.commands.registerCommand("artifactWorkflow.proceed", async () => {
      const activeUri = getActiveMarkdownUri();
      if (!activeUri) {
        vscode.window.showWarningMessage("Open a markdown artifact file to proceed.");
        return;
      }

      const unresolved = getUnresolvedCountForUri(activeUri);
      const cfg = vscode.workspace.getConfiguration("artifactWorkflow");
      const block = cfg.get<boolean>("blockProceedWithUnresolved", true);
      const mode = cfg.get<string>("mode", "manual");
      const autoInvoke = cfg.get<boolean>("autoInvokeCopilot", false);

      const stage = detectStageFromUri(activeUri);

      if (block && unresolved > 0) {
        await importInlineCommentsToReview(stage, activeUri);
        const prompt = buildCurrentStagePrompt(stage);
        await writeAgentTriggerFile(prompt);

        if (autoInvoke) {
          await tryInvokeCopilot(prompt);
        }

        const choice = await vscode.window.showWarningMessage(
          `Proceed blocked: ${unresolved} unresolved comment(s). Imported comments for current stage review.`,
          "Copy Agent Prompt",
          "Open Trigger File",
          "Open Comments File"
        );
        if (choice === "Copy Agent Prompt") {
          await vscode.env.clipboard.writeText(prompt);
          vscode.window.showInformationMessage("Current-stage agent prompt copied to clipboard.");
        }
        if (choice === "Open Trigger File") {
          const root = workspaceRoot();
          if (root) {
            const p = path.join(root, ".ai", "reviews", "agent-trigger.md");
            if (fs.existsSync(p)) {
              const doc = await vscode.workspace.openTextDocument(vscode.Uri.file(p));
              await vscode.window.showTextDocument(doc, { preview: false });
            }
          }
        }
        if (choice === "Open Comments File") {
          await vscode.commands.executeCommand("artifactWorkflow.openCommentsFile");
        }
        return;
      }

      if (mode === "manual") {
        const confirm = await vscode.window.showInformationMessage(
          "No blocking comments found. Proceed to next stage?",
          "Proceed",
          "Cancel"
        );
        if (confirm !== "Proceed") {
          return;
        }
      }

      if (stage === "unknown") {
        vscode.window.showInformationMessage("Proceed allowed. No mapped review gate for this file.");
        return;
      }

      const autoUpdate = cfg.get<boolean>("autoUpdateGateFiles", true);
      if (autoUpdate) {
        const updated = await markGateAsApproved(stage);
        if (!updated) {
          vscode.window.showWarningMessage("Proceed allowed, but gate file was not updated automatically.");
        }
      }

      const next = nextStage(stage);
      const nextPrompt = buildNextStagePrompt(stage);
      await writeAgentTriggerFile(nextPrompt);

      const action = await vscode.window.showInformationMessage(
        `Proceed approved for ${stage}. Next stage: ${next}.`,
        "Run Next Agent",
        "Copy Next Prompt",
        "Open Trigger File"
      );

      if (action === "Run Next Agent") {
        await tryInvokeCopilot(nextPrompt);
      }
      if (action === "Copy Next Prompt") {
        await vscode.env.clipboard.writeText(nextPrompt);
        vscode.window.showInformationMessage("Next-stage agent prompt copied to clipboard.");
      }
      if (action === "Open Trigger File") {
        const root = workspaceRoot();
        if (root) {
          const p = path.join(root, ".ai", "reviews", "agent-trigger.md");
          if (fs.existsSync(p)) {
            const doc = await vscode.workspace.openTextDocument(vscode.Uri.file(p));
            await vscode.window.showTextDocument(doc, { preview: false });
          }
        }
      }

      await refresh();
    })
  );

  void refresh();
}

async function tryInvokeCopilot(prompt: string): Promise<void> {
  try {
    await vscode.commands.executeCommand("workbench.action.chat.open");
    await vscode.env.clipboard.writeText(prompt);
    vscode.window.showInformationMessage("Chat opened. Prompt copied to clipboard; paste to run current-stage agent.");
  } catch {
    // Best-effort only.
  }
}

function buildCurrentStagePrompt(stage: ReviewStage): string {
  if (stage === "requirements") {
    return "Run AnalyzerAgent for docs/distilled_requirements.md and address imported inline comments from .ai/reviews/requirements.review.md.";
  }
  if (stage === "design") {
    return "Run ArchitectAgent for docs/system_design.md and address imported inline comments from .ai/reviews/design.review.md.";
  }
  if (stage === "epic") {
    return "Run ExecutionManagerAgent and revise docs/epics/00-epic_summary.md using comments in .ai/reviews/epic.review.md.";
  }
  if (stage === "tasks") {
    return "Run ExecutionManagerAgent and revise docs/epics task planning files using comments in .ai/reviews/tasks.review.md.";
  }
  return "Review stage unknown. Resolve comments manually.";
}

function buildNextStagePrompt(stage: ReviewStage): string {
  if (stage === "requirements") {
    return "Run ArchitectAgent for docs/system_design.md using approved requirements from docs/distilled_requirements.md and comments in .ai/reviews/design.review.md.";
  }
  if (stage === "design") {
    return "Run LeadArchitectAgent using docs/system_design.md and generate latest planning artifacts under docs/epics/.";
  }
  if (stage === "epic") {
    return "Run ExecutionManagerAgent and generate or revise docs/epics/00-task-planning-across-all-epics.md plus per-epic task files.";
  }
  if (stage === "tasks") {
    return "Run ExecutionManagerAgent for approved task IDs from docs/epics/00-task-planning-across-all-epics.md and orchestrate Coding/Testing/Security/Deployment.";
  }
  return "Proceed approved for unknown stage.";
}

async function writeAgentTriggerFile(prompt: string): Promise<void> {
  const root = workspaceRoot();
  if (!root) return;
  const dir = path.join(root, ".ai", "reviews");
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true });
  }
  const file = path.join(dir, "agent-trigger.md");
  const body = `# Agent Trigger\n\nGenerated: ${new Date().toISOString()}\n\n## Prompt\n${prompt}\n`;
  fs.writeFileSync(file, body, "utf8");
}

async function importInlineCommentsToReview(stage: ReviewStage, uri: vscode.Uri): Promise<void> {
  if (stage === "unknown") return;
  const root = workspaceRoot();
  if (!root) return;

  const reviewFileMap: Record<Exclude<ReviewStage, "unknown">, string> = {
    requirements: ".ai/reviews/requirements.review.md",
    design: ".ai/reviews/design.review.md",
    epic: ".ai/reviews/epic.review.md",
    tasks: ".ai/reviews/tasks.review.md"
  };

  const comments = extractMarkdownDocsComments(uri.fsPath);
  if (comments.length === 0) return;

  const reviewPath = path.join(root, reviewFileMap[stage]);
  if (!fs.existsSync(reviewPath)) return;

  const existing = fs.readFileSync(reviewPath, "utf8");
  const stamp = `\n## Imported Inline Comments (${new Date().toISOString()})\n`;
  const lines = comments
    .slice(0, 100)
    .map((c, i) => `- [ ] CMT-${i + 1} line ${c.line}: ${c.text}`)
    .join("\n");
  fs.writeFileSync(reviewPath, `${existing}${stamp}${lines}\n`, "utf8");
}

function extractMarkdownDocsComments(filePath: string): Array<{ line: number; text: string }> {
  try {
    const md = fs.readFileSync(filePath, "utf8");
    const regex = /(:+)comment(?:\[[^\]]*\])?\{([^}]*)\}/g;
    const out: Array<{ line: number; text: string }> = [];
    let match: RegExpExecArray | null;
    while ((match = regex.exec(md)) !== null) {
      const attrs = match[2] || "";
      const textMatch = attrs.match(/text\\?=\"((?:[^\"\\]|\\.)*)\"/);
      const text = (textMatch?.[1] || "Inline comment").replace(/__NEWLINE__/g, " ");
      const line = md.slice(0, match.index).split("\n").length;
      out.push({ line, text });
    }
    return out;
  } catch {
    return [];
  }
}

export function deactivate(): void {}

function workspaceRoot(): string | undefined {
  const folder = vscode.workspace.workspaceFolders?.[0];
  return folder?.uri.fsPath;
}

function getCommentsFilePath(): string | undefined {
  const root = workspaceRoot();
  const filename = vscode.workspace.getConfiguration("artifactWorkflow").get<string>("commentsFilename", "comments.json");

  if (root) {
    const workspacePath = path.join(root, filename);
    if (fs.existsSync(workspacePath)) {
      return workspacePath;
    }
  }

  // Fallback for users who keep comments in extension-global file.
  const home = process.env.HOME;
  if (home) {
    const globalPath = path.join(home, ".vscode", "extensions", "jjkorpershoek.textfile-comments-0.0.3", "comments.json");
    if (fs.existsSync(globalPath)) {
      return globalPath;
    }
  }

  if (!root) return undefined;
  return path.join(root, filename);
}

function getActiveMarkdownUri(): vscode.Uri | undefined {
  const editor = vscode.window.activeTextEditor;
  if (editor && editor.document.languageId === "markdown") {
    return editor.document.uri;
  }

  const activeTab = vscode.window.tabGroups.activeTabGroup.activeTab;
  if (!activeTab) return undefined;

  const input = activeTab.input;
  if (input instanceof vscode.TabInputText || input instanceof vscode.TabInputCustom) {
    const uri = input.uri;
    if (uri.fsPath.toLowerCase().endsWith(".md")) {
      return uri;
    }
  }

  return undefined;
}

function parseThreads(): StoredThread[] {
  const commentsPath = getCommentsFilePath();
  if (!commentsPath || !fs.existsSync(commentsPath)) return [];

  try {
    const raw = fs.readFileSync(commentsPath, "utf8").trim();
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as StoredThread[]) : [];
  } catch {
    return [];
  }
}

function getUnresolvedCountForUri(uri: vscode.Uri): number {
  const cfg = vscode.workspace.getConfiguration("artifactWorkflow");
  const source = cfg.get<string>("commentSource", "auto");

  const fromTextfile = source === "auto" || source === "textfile-comments"
    ? getUnresolvedCountFromTextfileComments(uri)
    : 0;

  const fromMarkdownDocs = source === "auto" || source === "markdown-docs"
    ? getUnresolvedCountFromMarkdownDocsDirectives(uri)
    : 0;

  return fromTextfile + fromMarkdownDocs;
}

function getUnresolvedCountFromTextfileComments(uri: vscode.Uri): number {
  const threads = parseThreads();
  const filePath = normalize(uri.fsPath);

  return threads
    .filter((t) => normalize(t.uri?.fsPath ?? "") === filePath)
    .filter((t) => {
      const cv = (t.contextValue ?? "").toLowerCase();
      if (cv === "resolved") return false;
      const labels = (t.comments ?? []).map((c) => (c.label ?? "").toLowerCase());
      if (labels.length > 0 && labels.every((l) => l === "resolved")) return false;
      return true;
    }).length;
}

function getUnresolvedCountFromMarkdownDocsDirectives(uri: vscode.Uri): number {
  try {
    const text = fs.readFileSync(uri.fsPath, "utf8");
    const directiveRegex = /(:+)comment(?:\[[^\]]*\])?\{([^}]*)\}/g;
    let count = 0;
    let match: RegExpExecArray | null;

    while ((match = directiveRegex.exec(text)) !== null) {
      const attrs = (match[2] || "").toLowerCase();
      const isResolved = /status\s*=\s*["']resolved["']/.test(attrs) || /resolved\s*=\s*["']true["']/.test(attrs);
      if (!isResolved) {
        count += 1;
      }
    }

    return count;
  } catch {
    return 0;
  }
}

function normalize(p: string): string {
  return path.resolve(p).toLowerCase();
}

function detectStageFromUri(uri: vscode.Uri): ReviewStage {
  const base = path.basename(uri.fsPath).toLowerCase();
  if (base === "requirements.review.md") return "requirements";
  if (base === "design.review.md") return "design";
  if (base === "epic.review.md") return "epic";
  if (base === "tasks.review.md") return "tasks";
  if (base.startsWith("distilled_requirements")) return "requirements";
  if (base.startsWith("system_design")) return "design";
  if (base.startsWith("epic_execution_plan") || base === "00-epic_summary.md") return "epic";
  if (
    base.includes("task_pack") ||
    base === "00-task-planning-across-all-epics.md" ||
    uri.fsPath.includes(`${path.sep}.ai${path.sep}tasks${path.sep}`)
  ) {
    return "tasks";
  }
  return "unknown";
}

function nextStage(stage: ReviewStage): string {
  if (stage === "requirements") return "design";
  if (stage === "design") return "epic";
  if (stage === "epic") return "tasks";
  if (stage === "tasks") return "execution";
  return "n/a";
}

async function markGateAsApproved(stage: ReviewStage): Promise<boolean> {
  const root = workspaceRoot();
  if (!root) return false;

  const mapping: Record<Exclude<ReviewStage, "unknown">, { file: string; from: string; to: string }> = {
    requirements: {
      file: ".ai/reviews/requirements.review.md",
      from: "- [ ] Proceed to Design",
      to: "- [x] Proceed to Design"
    },
    design: {
      file: ".ai/reviews/design.review.md",
      from: "- [ ] Proceed to Epic Planning",
      to: "- [x] Proceed to Epic Planning"
    },
    epic: {
      file: ".ai/reviews/epic.review.md",
      from: "- [ ] Proceed to Task Packs",
      to: "- [x] Proceed to Task Packs"
    },
    tasks: {
      file: ".ai/reviews/tasks.review.md",
      from: "- [ ] Proceed to Coding/Testing/Security/Deployment Execution",
      to: "- [x] Proceed to Coding/Testing/Security/Deployment Execution"
    }
  };

  if (stage === "unknown") return false;

  const gate = mapping[stage];
  const gatePath = path.join(root, gate.file);
  if (!fs.existsSync(gatePath)) return false;

  const content = fs.readFileSync(gatePath, "utf8");
  const next = content.includes(gate.from) ? content.replace(gate.from, gate.to) : content;
  if (next !== content) {
    fs.writeFileSync(gatePath, next, "utf8");
    const doc = await vscode.workspace.openTextDocument(vscode.Uri.file(gatePath));
    await doc.save();
  }

  return true;
}
