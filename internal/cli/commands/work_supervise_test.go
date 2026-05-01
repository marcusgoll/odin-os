package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
)

func TestRunWorkSuperviseStatusJSONReportsNoSideEffects(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	report := runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "status", "--json"})

	assertSuperviseReportShape(t, report)
	if report.Mode != supervision.ModeKeyStage7SupervisedAgency {
		t.Fatalf("mode = %q, want %q", report.Mode, supervision.ModeKeyStage7SupervisedAgency)
	}
	if report.Enabled {
		t.Fatalf("enabled = true, want default stopped state")
	}
	if !report.KillSwitch {
		t.Fatalf("kill_switch = false, want default stopped state to keep kill switch active")
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseStartJSONReportsNoSideEffects(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	report := runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})

	assertSuperviseReportShape(t, report)
	if !report.Enabled || report.KillSwitch {
		t.Fatalf("control = enabled %t kill_switch %t, want enabled without kill switch", report.Enabled, report.KillSwitch)
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseStopJSONReportsNoSideEffects(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	report := runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "stop", "--json"})

	assertSuperviseReportShape(t, report)
	if report.Enabled || !report.KillSwitch {
		t.Fatalf("control = enabled %t kill_switch %t, want stopped with kill switch", report.Enabled, report.KillSwitch)
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseQueueProjectJSONRecordsDecisionsWithoutStartingWork(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	report := runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "queue", "--project", "alpha", "--json"})

	assertSuperviseReportShape(t, report)
	if len(report.Queue) != 1 {
		t.Fatalf("queue len = %d, want 1 decision: %+v", len(report.Queue), report.Queue)
	}
	decision := report.Queue[0]
	if decision.ProjectKey != "alpha" || decision.Repo != "acme/alpha" || decision.IssueNumber == 0 {
		t.Fatalf("decision target = %+v, want alpha/acme/alpha issue", decision)
	}
	if decision.Decision != supervision.DecisionEligible || !decision.Eligible || decision.ClaimKey == "" {
		t.Fatalf("decision = %+v, want eligible reserved claim", decision)
	}
	if len(report.Claims) != 1 || report.Claims[0].Status != supervision.ClaimStatusReserved {
		t.Fatalf("claims = %+v, want one reserved claim", report.Claims)
	}

	decisions, err := store.ListSupervisionQueueDecisions(ctx, sqlite.ListSupervisionQueueDecisionsParams{
		Repo: "acme/alpha",
	})
	if err != nil {
		t.Fatalf("ListSupervisionQueueDecisions() error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].Decision != supervision.DecisionEligible {
		t.Fatalf("stored decisions = %+v, want one eligible decision", decisions)
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseRecoverJSONReportsNoSideEffects(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	report := runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "recover", "--json"})

	assertSuperviseReportShape(t, report)
	if report.Recovery.Status != supervision.RecoveryStatusClean || report.Recovery.ActiveClaims != 0 {
		t.Fatalf("recovery = %+v, want clean with zero active claims", report.Recovery)
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseUnknownSubcommandShowsUsage(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	var output strings.Builder
	if err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{"supervise", "bogus"}, &output); err != nil {
		t.Fatalf("RunWork(supervise bogus) error = %v", err)
	}
	if !strings.Contains(output.String(), "unknown work supervise command: bogus") || !strings.Contains(output.String(), "usage: odin work supervise status|start|stop|queue --project <key>|recover [--json]") {
		t.Fatalf("output = %q, want unknown subcommand usage", output.String())
	}
}

type superviseCommandReport struct {
	Mode       string `json:"mode"`
	Enabled    bool   `json:"enabled"`
	KillSwitch bool   `json:"kill_switch"`
	ConfigHash string `json:"config_hash"`
	Queue      []struct {
		ProjectKey    string `json:"project_key"`
		Repo          string `json:"repo"`
		IssueNumber   int    `json:"issue_number"`
		Decision      string `json:"decision"`
		Eligible      bool   `json:"eligible"`
		RefusalReason string `json:"refusal_reason,omitempty"`
		ClaimKey      string `json:"claim_key,omitempty"`
	} `json:"queue"`
	Claims []struct {
		ProjectKey  string `json:"project_key"`
		Repo        string `json:"repo"`
		IssueNumber int    `json:"issue_number"`
		ClaimKey    string `json:"claim_key"`
		Status      string `json:"status"`
	} `json:"claims"`
	Recovery struct {
		Status       string `json:"status"`
		Reason       string `json:"reason"`
		ActiveClaims int    `json:"active_claims"`
	} `json:"recovery"`
	CodexExecution string `json:"codex_execution"`
	PRs            string `json:"prs"`
	Merge          string `json:"merge"`
	Deployment     string `json:"deployment"`
}

func runWorkSuperviseJSON(t *testing.T, ctx context.Context, store *sqlite.Store, args []string) superviseCommandReport {
	t.Helper()

	var output strings.Builder
	if err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, args, &output); err != nil {
		t.Fatalf("RunWork(%v) error = %v\noutput:\n%s", args, err, output.String())
	}
	if strings.Contains(output.String(), "github_writes") || strings.Contains(output.String(), "method_audit") {
		t.Fatalf("output includes GitHub write audit fields:\n%s", output.String())
	}

	var report superviseCommandReport
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput:\n%s", err, output.String())
	}
	return report
}

func assertSuperviseReportShape(t *testing.T, report superviseCommandReport) {
	t.Helper()

	if report.Mode == "" || report.ConfigHash == "" {
		t.Fatalf("report mode/config_hash missing: %+v", report)
	}
	if report.CodexExecution != supervision.SideEffectNotStarted ||
		report.PRs != supervision.SideEffectNotCreated ||
		report.Merge != supervision.SideEffectNotMerged ||
		report.Deployment != supervision.SideEffectNotStarted {
		t.Fatalf("side effects = codex %q prs %q merge %q deployment %q, want no worker/PR/merge/deploy action",
			report.CodexExecution,
			report.PRs,
			report.Merge,
			report.Deployment,
		)
	}
}

func assertNoSuperviseSideEffects(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

	for _, table := range []string{"runs", "approvals", "worktree_leases"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want no supervise side-effect rows", table, count)
		}
	}
}
