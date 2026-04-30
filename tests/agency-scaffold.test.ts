import { describe, expect, it } from "vitest";

import { createDefaultAgencyApp } from "../src/index.js";
import { renderStatus } from "../src/dashboard/index.js";
import { denyProductionSecretAccess } from "../src/security/index.js";

describe("agency scaffold", () => {
  it("creates a dry-run app without dispatching workers", async () => {
    const app = await createDefaultAgencyApp();
    const plan = await app.planOnce();

    expect(plan).toEqual({
      dryRun: true,
      killSwitch: false,
      eligibleIssueCount: 0,
      plannedRunCount: 0,
      reason: "dry_run_only"
    });
  });

  it("renders operator status text", () => {
    const status = renderStatus({
      dryRun: true,
      killSwitch: false,
      eligibleIssueCount: 2,
      plannedRunCount: 0,
      reason: "dry_run_only"
    });

    expect(status).toContain("dry_run=true");
    expect(status).toContain("eligible=2");
  });

  it("denies production secret-like paths", () => {
    expect(denyProductionSecretAccess("/srv/app/.env.production")).toEqual({
      allowed: false,
      reason: "production_secret_or_env_path_denied"
    });
  });
});
