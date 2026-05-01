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

func TestRunWorkSuperviseQueueProjectFixtureJSONReportsDecisionWithoutDurableQueueState(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	report := runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "queue", "--project", "alpha", "--fixture-issue", "7", "--json"})

	assertSuperviseReportShape(t, report)
	if report.Source != "control_plane_fixture" {
		t.Fatalf("source = %q, want control_plane_fixture", report.Source)
	}
	if len(report.Queue) != 1 {
		t.Fatalf("queue len = %d, want 1 decision: %+v", len(report.Queue), report.Queue)
	}
	decision := report.Queue[0]
	if decision.ProjectKey != "alpha" || decision.Repo != "acme/alpha" || decision.IssueNumber != 7 {
		t.Fatalf("decision target = %+v, want alpha/acme/alpha fixture issue 7", decision)
	}
	if decision.Decision != supervision.DecisionEligible || !decision.Eligible || decision.ClaimKey != "" {
		t.Fatalf("decision = %+v, want eligible fixture decision without durable claim", decision)
	}
	if len(report.Claims) != 0 {
		t.Fatalf("claims = %+v, want no durable fixture claims", report.Claims)
	}

	assertSuperviseTableCount(t, ctx, store, "projects", 0)
	assertSuperviseTableCount(t, ctx, store, "supervision_queue_decisions", 0)
	assertSuperviseTableCount(t, ctx, store, "supervision_dispatch_claims", 0)
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseQueueWithoutFixtureIssueFailsWithoutCreatingClaims(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})

	var output strings.Builder
	err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{"supervise", "queue", "--project", "alpha", "--json"}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise queue without fixture) error = nil, want fixture boundary error\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "--fixture-issue is required for work supervise queue in this slice") {
		t.Fatalf("error = %q, want required fixture issue error", err.Error())
	}
	assertSuperviseTableCount(t, ctx, store, "projects", 0)
	assertSuperviseTableCount(t, ctx, store, "supervision_queue_decisions", 0)
	assertSuperviseTableCount(t, ctx, store, "supervision_dispatch_claims", 0)
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

func TestRunWorkSuperviseStartWithoutJSONFailsWithoutMutatingControlState(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	var output strings.Builder
	err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{"supervise", "start"}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise start without --json) error = nil, want required JSON error\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "--json is required for work supervise in this slice") {
		t.Fatalf("error = %q, want required JSON error", err.Error())
	}

	report := runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "status", "--json"})
	if report.Enabled || !report.KillSwitch {
		t.Fatalf("control mutated after missing --json: enabled=%t kill_switch=%t", report.Enabled, report.KillSwitch)
	}
}

func TestRunWorkSuperviseUnknownSubcommandShowsUsage(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	var output strings.Builder
	if err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{"supervise", "bogus"}, &output); err != nil {
		t.Fatalf("RunWork(supervise bogus) error = %v", err)
	}
	if !strings.Contains(output.String(), "unknown work supervise command: bogus") || !strings.Contains(output.String(), "usage: odin work supervise status|start|stop|queue --project <key> --fixture-issue <number>|recover --json") {
		t.Fatalf("output = %q, want unknown subcommand usage", output.String())
	}
}

type superviseCommandReport struct {
	Mode       string `json:"mode"`
	Source     string `json:"source,omitempty"`
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
		assertSuperviseTableCount(t, ctx, store, table, 0)
	}
}

func assertSuperviseTableCount(t *testing.T, ctx context.Context, store *sqlite.Store, table string, want int) {
	t.Helper()

	var count int
	if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}
