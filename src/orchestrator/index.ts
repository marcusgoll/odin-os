import type { AgencyConfig } from "../config/index.js";
import type { RuntimeStore } from "../db/index.js";
import type { StructuredLogger } from "../logging/index.js";
import type { PromptRenderer } from "../prompts/index.js";
import type { AgentRunner } from "../runner/index.js";
import type { IssueTracker } from "../tracker/github/index.js";
import type { WorkspaceManager } from "../workspace/index.js";
import type { SchedulerPlan } from "./types.js";

export interface AgencyOrchestratorDependencies {
  readonly config: AgencyConfig;
  readonly logger: StructuredLogger;
  readonly store: RuntimeStore;
  readonly tracker: IssueTracker;
  readonly workspaceManager: WorkspaceManager;
  readonly runner: AgentRunner;
  readonly promptRenderer: PromptRenderer;
}

export interface AgencyOrchestrator {
  planOnce(): Promise<SchedulerPlan>;
}

export function createAgencyOrchestrator(deps: AgencyOrchestratorDependencies): AgencyOrchestrator {
  return {
    async planOnce() {
      if (deps.config.runtime.killSwitch) {
        deps.logger.warn("agency dispatch disabled by kill switch");
        return {
          dryRun: deps.config.runtime.dryRun,
          killSwitch: true,
          eligibleIssueCount: 0,
          plannedRunCount: 0,
          reason: "kill_switch_active"
        };
      }

      const issues = await deps.tracker.listEligibleIssues();

      return {
        dryRun: deps.config.runtime.dryRun,
        killSwitch: false,
        eligibleIssueCount: issues.length,
        plannedRunCount: deps.config.runtime.dryRun ? 0 : issues.length,
        reason: deps.config.runtime.dryRun ? "dry_run_only" : "dispatch_not_implemented"
      };
    }
  };
}
