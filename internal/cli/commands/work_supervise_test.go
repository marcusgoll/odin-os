package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
	trackerintake "odin-os/internal/tracker/intake"
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

func TestRunWorkSuperviseQueueJSONEvaluatesTrackerIssuesWithoutGitHubWritesOrDispatch(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	projectRegistry := commandProjectRegistry(t)

	previousFactory := newIntakeTracker
	t.Cleanup(func() { newIntakeTracker = previousFactory })
	newIntakeTracker = func(project projects.Manifest, options trackerintake.SyncOptions) (tracker.Tracker, error) {
		if project.GitHub.Repo != "acme/alpha" {
			return nil, fmt.Errorf("repo = %q, want acme/alpha", project.GitHub.Repo)
		}
		return &commandAuditedFakeTracker{
			issues: []tracker.Issue{
				{
					Provider: "github",
					Repo:     "acme/alpha",
					Number:   21,
					Title:    "Update supervised queue docs",
					Body:     "Planned scope: docs/example.md",
					URL:      "https://github.example/acme/alpha/issues/21",
					State:    "open",
					Labels:   []string{"odin:ready", "safety:low-risk"},
				},
				{
					Provider: "github",
					Repo:     "acme/alpha",
					Number:   22,
					Title:    "Touch sensitive policy test",
					Body:     "Planned scope: internal/security/policy_test.go",
					URL:      "https://github.example/acme/alpha/issues/22",
					State:    "open",
					Labels:   []string{"odin:ready", "safety:low-risk"},
				},
				{
					Provider: "github",
					Repo:     "acme/alpha",
					Number:   23,
					Title:    "Investigate unknown queue scope",
					Body:     "Planned scope is unknown for this request.",
					URL:      "https://github.example/acme/alpha/issues/23",
					State:    "open",
					Labels:   []string{"odin:ready", "safety:low-risk"},
				},
				{
					Provider: "github",
					Repo:     "acme/alpha",
					Number:   24,
					Title:    "Missing safety label",
					Body:     "Planned scope: docs/missing-label.md",
					URL:      "https://github.example/acme/alpha/issues/24",
					State:    "open",
					Labels:   []string{"odin:ready"},
				},
			},
			audit: tracker.RequestAudit{Reads: 1},
		}, nil
	}

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	report := runWorkSuperviseJSONWithRegistry(t, ctx, store, projectRegistry, []string{"supervise", "queue", "--project", "alpha", "--json"})

	assertSuperviseReportShape(t, report)
	if report.Source == "control_plane_fixture" {
		t.Fatalf("source = %q, want tracker-backed queue source", report.Source)
	}
	if len(report.Queue) != 4 {
		t.Fatalf("queue len = %d, want 4 decisions: %+v", len(report.Queue), report.Queue)
	}
	assertSuperviseDecision(t, report.Queue[0], 21, supervision.DecisionEligible, "")
	assertSuperviseDecision(t, report.Queue[1], 22, supervision.DecisionRefused, supervision.RefusalSensitiveTestScope)
	assertSuperviseDecision(t, report.Queue[2], 23, supervision.DecisionRefused, supervision.RefusalUnknownScope)
	assertSuperviseDecision(t, report.Queue[3], 24, supervision.DecisionRefused, supervision.RefusalMissingRequiredLabel)

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	decisions, err := store.ListSupervisionQueueDecisions(ctx, sqlite.ListSupervisionQueueDecisionsParams{
		ProjectID: &project.ID,
		Repo:      "acme/alpha",
	})
	if err != nil {
		t.Fatalf("ListSupervisionQueueDecisions() error = %v", err)
	}
	if len(decisions) != 4 {
		t.Fatalf("persisted decisions len = %d, want 4: %+v", len(decisions), decisions)
	}
	wantReasons := map[int]string{
		21: supervision.DecisionEligible,
		22: supervision.RefusalSensitiveTestScope,
		23: supervision.RefusalUnknownScope,
		24: supervision.RefusalMissingRequiredLabel,
	}
	for _, decision := range decisions {
		if decision.Reason != wantReasons[decision.IssueNumber] {
			t.Fatalf("persisted decision for issue %d reason = %q, want %q", decision.IssueNumber, decision.Reason, wantReasons[decision.IssueNumber])
		}
	}
	assertSuperviseTableCount(t, ctx, store, "tasks", 0)
	assertSuperviseTableCount(t, ctx, store, "runs", 0)
	assertSuperviseTableCount(t, ctx, store, "approvals", 0)
	assertSuperviseTableCount(t, ctx, store, "worktree_leases", 0)
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseQueueJSONPersistsIssueBodyHashWithoutRawBody(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	projectRegistry := commandProjectRegistry(t)

	sensitiveBody := strings.Join([]string{
		"Planned scope: docs/example.md",
		"Failure dump: leaked/ghp_123456789012345678901234567890123456.txt",
		"Planned scope: leaked/ghp_123456789012345678901234567890123456.txt",
	}, "\n")
	previousFactory := newIntakeTracker
	t.Cleanup(func() { newIntakeTracker = previousFactory })
	newIntakeTracker = func(project projects.Manifest, options trackerintake.SyncOptions) (tracker.Tracker, error) {
		return &commandAuditedFakeTracker{
			issues: []tracker.Issue{{
				Provider: "github",
				Repo:     project.GitHub.Repo,
				Number:   26,
				Title:    "Hash sensitive body evidence",
				Body:     sensitiveBody,
				URL:      "https://github.example/acme/alpha/issues/26",
				State:    "open",
				Labels:   []string{"odin:ready", "safety:low-risk"},
			}},
			audit: tracker.RequestAudit{Reads: 1},
		}, nil
	}

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	report := runWorkSuperviseJSONWithRegistry(t, ctx, store, projectRegistry, []string{"supervise", "queue", "--project", "alpha", "--json"})
	if len(report.Queue) != 1 {
		t.Fatalf("queue len = %d, want 1: %+v", len(report.Queue), report.Queue)
	}
	assertSuperviseDecision(t, report.Queue[0], 26, supervision.DecisionRefused, supervision.RefusalForbiddenPath)
	if len(report.Claims) != 0 {
		t.Fatalf("claims = %+v, want no claim for sensitive mixed scope", report.Claims)
	}

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	decisions, err := store.ListSupervisionQueueDecisions(ctx, sqlite.ListSupervisionQueueDecisionsParams{
		ProjectID: &project.ID,
		Repo:      "acme/alpha",
	})
	if err != nil {
		t.Fatalf("ListSupervisionQueueDecisions() error = %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("persisted decisions len = %d, want 1: %+v", len(decisions), decisions)
	}

	decisionJSON := decisions[0].DecisionJSON
	if strings.Contains(decisionJSON, sensitiveBody) || strings.Contains(decisionJSON, "ghp_123456789012345678901234567890123456") || strings.Contains(decisionJSON, "leaked/ghp_123456789012345678901234567890123456.txt") {
		t.Fatalf("decision_json leaked raw issue body/token-like content: %s", decisionJSON)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(decisionJSON), &payload); err != nil {
		t.Fatalf("json.Unmarshal(decision_json) error = %v\njson: %s", err, decisionJSON)
	}
	if _, ok := payload["issue_body"]; ok {
		t.Fatalf("decision_json contains raw issue_body field: %s", decisionJSON)
	}
	wantHash := "sha256:" + sha256HexForSuperviseTest(sensitiveBody)
	if !strings.Contains(decisionJSON, `"issue_body_hash":"`+wantHash+`"`) {
		t.Fatalf("decision_json = %s, want issue_body_hash %q", decisionJSON, wantHash)
	}
	changedPaths, ok := payload["changed_paths"].([]any)
	if !ok {
		t.Fatalf("decision_json = %s, want changed_paths array", decisionJSON)
	}
	if !containsJSONStrings(changedPaths, "docs/example.md", "internal/security/redacted-sensitive-path.txt") {
		t.Fatalf("changed_paths = %+v, want allowed path and redacted refusal marker", changedPaths)
	}
}

func TestRunWorkSuperviseQueueJSONFailsWhenTrackerAuditObservesGitHubWrite(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	projectRegistry := commandProjectRegistry(t)

	previousFactory := newIntakeTracker
	t.Cleanup(func() { newIntakeTracker = previousFactory })
	newIntakeTracker = func(project projects.Manifest, options trackerintake.SyncOptions) (tracker.Tracker, error) {
		return &commandAuditedFakeTracker{
			issues: []tracker.Issue{{
				Provider: "github",
				Repo:     project.GitHub.Repo,
				Number:   25,
				Title:    "Unexpected write audit",
				Body:     "Planned scope: docs/write-audit.md",
				State:    "open",
				Labels:   []string{"odin:ready", "safety:low-risk"},
			}},
			audit: tracker.RequestAudit{
				Reads:  1,
				Writes: 1,
				Forbidden: []tracker.ForbiddenRequest{{
					Method: "POST",
					Path:   "/repos/acme/alpha/issues/25/comments",
				}},
			},
		}, nil
	}

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})

	var output strings.Builder
	err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{"supervise", "queue", "--project", "alpha", "--json"}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise queue with write audit) error = nil, want forbidden GitHub write failure\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "forbidden GitHub write attempted during supervise queue intake") || strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error = %q, want safe forbidden-write message", err.Error())
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
	if !strings.Contains(output.String(), "unknown work supervise command: bogus") || !strings.Contains(output.String(), "usage: odin work supervise status|start|stop|queue --project <key> [--fixture-issue <number>]|recover --json") {
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
	return runWorkSuperviseJSONWithRegistry(t, ctx, store, commandProjectRegistry(t), args)
}

func runWorkSuperviseJSONWithRegistry(t *testing.T, ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string) superviseCommandReport {
	t.Helper()

	var output strings.Builder
	if err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, args, &output); err != nil {
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

func assertSuperviseDecision(t *testing.T, decision struct {
	ProjectKey    string `json:"project_key"`
	Repo          string `json:"repo"`
	IssueNumber   int    `json:"issue_number"`
	Decision      string `json:"decision"`
	Eligible      bool   `json:"eligible"`
	RefusalReason string `json:"refusal_reason,omitempty"`
	ClaimKey      string `json:"claim_key,omitempty"`
}, issueNumber int, decisionValue string, refusalReason string) {
	t.Helper()

	if decision.ProjectKey != "alpha" || decision.Repo != "acme/alpha" || decision.IssueNumber != issueNumber {
		t.Fatalf("decision target = %+v, want alpha/acme/alpha issue %d", decision, issueNumber)
	}
	if decision.Decision != decisionValue || decision.RefusalReason != refusalReason {
		t.Fatalf("decision = %+v, want decision %q refusal %q", decision, decisionValue, refusalReason)
	}
	if decisionValue == supervision.DecisionEligible {
		if !decision.Eligible || decision.ClaimKey == "" {
			t.Fatalf("decision = %+v, want eligible decision with planned claim", decision)
		}
		return
	}
	if decision.Eligible || decision.ClaimKey != "" {
		t.Fatalf("decision = %+v, want refused decision without claim", decision)
	}
}

func sha256HexForSuperviseTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func containsJSONStrings(values []any, expected ...string) bool {
	present := make(map[string]bool, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		present[text] = true
	}
	for _, value := range expected {
		if !present[value] {
			return false
		}
	}
	return true
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
