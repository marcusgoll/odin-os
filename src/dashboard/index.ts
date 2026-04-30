import type { SchedulerPlan } from "../orchestrator/types.js";

export function renderStatus(plan: SchedulerPlan): string {
  return `Agency dry_run=${plan.dryRun} kill_switch=${plan.killSwitch} eligible=${plan.eligibleIssueCount} planned=${plan.plannedRunCount}`;
}
