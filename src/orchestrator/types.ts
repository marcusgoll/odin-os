export type WorkItemStatus = "open" | "blocked" | "running" | "human_review" | "done" | "failed";

export interface WorkItem {
  readonly id: string;
  readonly projectKey: string;
  readonly title: string;
  readonly status: WorkItemStatus;
  readonly source: {
    readonly provider: "github";
    readonly externalId: string;
    readonly url: string;
  };
}

export interface RunAttempt {
  readonly id: string;
  readonly workItemId: string;
  readonly role: "triage" | "planner" | "builder" | "qa" | "reviewer" | "maintainer";
  readonly status: "planned" | "running" | "completed" | "failed" | "interrupted";
}

export interface SchedulerPlan {
  readonly dryRun: boolean;
  readonly killSwitch: boolean;
  readonly eligibleIssueCount: number;
  readonly plannedRunCount: number;
  readonly reason: string;
}
