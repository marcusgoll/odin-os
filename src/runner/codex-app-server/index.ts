import type { AgentRunner } from "../index.js";

export function createCodexAppServerRunner(): AgentRunner {
  return {
    kind: "codex_app_server",
    async run(request) {
      return {
        status: "interrupted",
        summary: `codex app-server placeholder for ${request.runAttempt.id}; execution is not implemented`
      };
    }
  };
}
