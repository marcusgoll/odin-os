export interface AgencyConfig {
  readonly runtime: {
    readonly dryRun: boolean;
    readonly killSwitch: boolean;
    readonly workspaceRoot: string;
  };
  readonly logging: {
    readonly level: "debug" | "info" | "warn" | "error";
  };
  readonly runners: {
    readonly codexExec: {
      readonly command: string;
    };
  };
}

export function loadConfig(): AgencyConfig {
  return {
    runtime: {
      dryRun: true,
      killSwitch: process.env["ODIN_AGENCY_KILL_SWITCH"] === "1",
      workspaceRoot: "workspaces"
    },
    logging: {
      level: "info"
    },
    runners: {
      codexExec: {
        command: "codex"
      }
    }
  };
}
