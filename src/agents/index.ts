export const agencyRoles = ["triage", "planner", "builder", "qa", "reviewer", "maintainer"] as const;

export type AgencyRole = (typeof agencyRoles)[number];
