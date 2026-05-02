package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

const workspaceUsage = "usage: odin workspace status [--json]|list [--json]"

func RunWorkspace(ctx context.Context, store *sqlite.Store, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, err := fmt.Fprintln(stdout, workspaceUsage)
		return err
	}

	switch args[0] {
	case "status":
		return runWorkspaceStatus(ctx, store, args[1:], stdout)
	case "list":
		return runWorkspaceList(ctx, store, args[1:], stdout)
	default:
		_, err := fmt.Fprintf(stdout, "unknown workspace command: %s\n%s\n", args[0], workspaceUsage)
		return err
	}
}

func runWorkspaceStatus(ctx context.Context, store *sqlite.Store, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	views, err := projections.ListActiveWorktreeLeaseViews(ctx, store.DB())
	if err != nil {
		return err
	}
	report := map[string]any{
		"surface":                 "odin workspace status",
		"status":                  "read_only",
		"active_worktree_leases":  len(views),
		"live_execution_sessions": "not_implemented",
		"adoption":                "not_implemented",
		"attach":                  "not_implemented",
		"handoff":                 "not_implemented",
		"stop":                    "not_implemented",
	}
	if parseBoolFlag(params, "json") {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	if _, err := fmt.Fprintln(stdout, "Workspace Status"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "Operator Surface: odin workspace ..."); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Worktree Leases: active=%d\n", len(views)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "Live Execution Sessions: not_implemented"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "Adoption: not_implemented"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "Attach: not_implemented"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "Handoff: not_implemented"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "Stop: not_implemented"); err != nil {
		return err
	}
	return nil
}

func runWorkspaceList(ctx context.Context, store *sqlite.Store, args []string, stdout io.Writer) error {
	params := parseWorkStartArgs(args)
	views, err := projections.ListActiveWorktreeLeaseViews(ctx, store.DB())
	if err != nil {
		return err
	}
	if parseBoolFlag(params, "json") {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]any{
			"surface":         "odin workspace list",
			"status":          "read_only",
			"active_leases":   len(views),
			"worktree_leases": views,
		})
	}

	if _, err := fmt.Fprintln(stdout, "Workspace Leases"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "Operator Surface: odin workspace ..."); err != nil {
		return err
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(stdout, "  none")
		return err
	}
	for _, view := range views {
		if _, err := fmt.Fprintf(
			stdout,
			"  lease=%d state=%s mode=%s project=%s project_id=%d work_item=%s run_attempt=%d branch=%s worktree=%s heartbeat=%s\n",
			view.LeaseID,
			view.State,
			view.Mode,
			view.ProjectKey,
			view.ProjectID,
			view.TaskKey,
			view.RunID,
			view.BranchName,
			view.WorktreePath,
			view.HeartbeatAt,
		); err != nil {
			return err
		}
	}
	return nil
}
