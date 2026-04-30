import type { RunAttempt } from "../orchestrator/types.js";

export interface AgentRunRequest {
  readonly runAttempt: RunAttempt;
  readonly prompt: string;
  readonly cwd: string;
}

export interface AgentRunResult {
  readonly status: "completed" | "failed" | "interrupted";
  readonly summary: string;
}

export interface AgentRunner {
  readonly kind: "codex_exec" | "codex_app_server";
  run(request: AgentRunRequest): Promise<AgentRunResult>;
}
