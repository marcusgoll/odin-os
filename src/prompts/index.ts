import type { RunAttempt, WorkItem } from "../orchestrator/types.js";

export interface PromptRequest {
  readonly workItem: WorkItem;
  readonly runAttempt: RunAttempt;
}

export interface PromptRenderer {
  render(request: PromptRequest): string;
}

export function createPromptRenderer(): PromptRenderer {
  return {
    render(request) {
      return [
        `Role: ${request.runAttempt.role}`,
        `Work Item: ${request.workItem.id}`,
        "Boundary: do not merge, deploy, or access production secrets."
      ].join("\n");
    }
  };
}
