import type { AgentRunner } from "../index.js";

export function createCodexExecRunner(options: { readonly command: string }): AgentRunner {
  return {
    kind: "codex_exec",
    async run(request) {
      return {
        status: "interrupted",
        summary: `${options.command} exec placeholder for ${request.runAttempt.id}; execution is not implemented`
      };
    }
  };
}
