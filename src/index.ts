import { loadConfig } from "./config/index.js";
import { createPlaceholderStore } from "./db/index.js";
import { createConsoleLogger } from "./logging/index.js";
import { createAgencyOrchestrator } from "./orchestrator/index.js";
import { createPromptRenderer } from "./prompts/index.js";
import { createCodexExecRunner } from "./runner/codex-exec/index.js";
import { createGithubIssueTracker } from "./tracker/github/index.js";
import { createWorkspaceManager } from "./workspace/index.js";

export async function createDefaultAgencyApp() {
  const config = loadConfig();
  const logger = createConsoleLogger({ level: config.logging.level });
  const store = createPlaceholderStore();
  const tracker = createGithubIssueTracker({ dryRun: config.runtime.dryRun });
  const workspaceManager = createWorkspaceManager({ root: config.runtime.workspaceRoot });
  const runner = createCodexExecRunner({ command: config.runners.codexExec.command });
  const promptRenderer = createPromptRenderer();

  return createAgencyOrchestrator({
    config,
    logger,
    store,
    tracker,
    workspaceManager,
    runner,
    promptRenderer
  });
}

export async function main(): Promise<void> {
  const app = await createDefaultAgencyApp();
  const plan = await app.planOnce();

  // The scaffold is intentionally dry-run only. Real dispatch belongs to later phases.
  console.log(JSON.stringify(plan, null, 2));
}

if (process.argv[1] && import.meta.url === new URL(process.argv[1], "file:").href) {
  await main();
}
