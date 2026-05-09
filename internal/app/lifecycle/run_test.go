package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"odin-os/internal/app/bootstrap"
	clioverview "odin-os/internal/cli/overview"
	"odin-os/internal/cli/tui"
	"odin-os/internal/core/capabilities"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/runtime/checkpoints"
	runtimestate "odin-os/internal/runtime/state"
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
	"odin-os/internal/vcs/worktrees"
)

const testProjectKey = "alpha-cli"

func TestServeDashboardAdminKillSwitchUpdatesReadinessAndRuntimeState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeRoot := t.TempDir()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	stateService := runtimestate.Service{Store: store}
	if _, err := stateService.MarkBooting(ctx, runtimestate.BootInput{BootID: "boot-admin", PID: 1234}); err != nil {
		t.Fatalf("MarkBooting() error = %v", err)
	}

	var immediate atomic.Bool
	var logBuffer bytes.Buffer
	admin := serveDashboardAdmin{
		ImmediateNotReady: &immediate,
		RuntimeState:      stateService,
		BootID:            "boot-admin",
		RuntimeRoot:       runtimeRoot,
		Logger:            &logs.Logger{Writer: &logBuffer},
	}

	if err := admin.KillSwitchOn(ctx); err != nil {
		t.Fatalf("KillSwitchOn() error = %v", err)
	}
	if !immediate.Load() {
		t.Fatal("ImmediateNotReady = false, want true after kill switch on")
	}
	reason, active, err := readReadinessFlag(runtimeRoot)
	if err != nil {
		t.Fatalf("readReadinessFlag() error = %v", err)
	}
	if !active || reason != "dashboard kill switch enabled" {
		t.Fatalf("readiness flag active=%v reason=%q, want dashboard kill switch enabled", active, reason)
	}
	runtimeState, err := store.GetRuntimeState(ctx)
	if err != nil {
		t.Fatalf("GetRuntimeState() error = %v", err)
	}
	if runtimeState.Status != "degraded" || runtimeState.LastError != "dashboard kill switch enabled" {
		t.Fatalf("runtime state = %+v, want degraded kill switch state", runtimeState)
	}
	if !strings.Contains(logBuffer.String(), "kill switch enabled") {
		t.Fatalf("log output = %q, want kill switch enabled event", logBuffer.String())
	}

	if err := admin.KillSwitchOff(ctx); err != nil {
		t.Fatalf("KillSwitchOff() error = %v", err)
	}
	if immediate.Load() {
		t.Fatal("ImmediateNotReady = true, want false after kill switch off")
	}
	if _, active, err := readReadinessFlag(runtimeRoot); err != nil || active {
		t.Fatalf("readiness flag after off active=%v err=%v, want inactive", active, err)
	}
}

func TestRunReplStartsInteractiveShell(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)

	stdin := strings.NewReader("/help\n")
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"repl"}, stdin, &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "scope=") {
		t.Fatalf("Run() output = %q, want header", output)
	}
	if !strings.Contains(output, "/help") {
		t.Fatalf("Run() output = %q, want help", output)
	}
}

func TestRunWithoutArgsPrintsUsageInsteadOfStartingShell(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, nil, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Usage: odin") {
		t.Fatalf("stdout = %q, want usage banner", output)
	}
	if strings.Contains(output, "odin>") {
		t.Fatalf("stdout = %q, should not contain repl prompt", output)
	}
}

func TestRunLeasesCleanupDryRunUsesCanonicalCommandPath(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"leases", "cleanup", "--dry-run"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run(leases cleanup --dry-run) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "no worktree leases") {
		t.Fatalf("stdout = %q, want no worktree leases", stdout.String())
	}
}

func TestRunStatusJSON(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	seedStatusCompanionSwarms(t, ctx, store)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer

	err = Run(context.Background(), root, []string{"status", "--json"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload struct {
		Health           string `json:"health"`
		PendingApprovals int    `json:"pending_approvals"`
		RegistryHealthy  bool   `json:"registry_healthy"`
		CompanionSwarms  []struct {
			ParentTaskKey       string `json:"parent_task_key"`
			Status              string `json:"status"`
			BlockedReason       string `json:"blocked_reason"`
			BacklogCount        int    `json:"backlog_count"`
			ActiveChildRunCount int    `json:"active_child_run_count"`
		} `json:"companion_swarms"`
		CompanionSwarmCounts struct {
			Active  int `json:"active"`
			Blocked int `json:"blocked"`
			Backlog int `json:"backlog"`
		} `json:"companion_swarm_counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("status json = %v", err)
	}
	if payload.Health == "" {
		t.Fatalf("Health = %q, want non-empty", payload.Health)
	}
	if !payload.RegistryHealthy {
		t.Fatalf("RegistryHealthy = false, want true")
	}
	if len(payload.CompanionSwarms) != 3 {
		t.Fatalf("CompanionSwarms len = %d, want 3", len(payload.CompanionSwarms))
	}
	if payload.CompanionSwarmCounts.Active != 1 {
		t.Fatalf("CompanionSwarmCounts.Active = %d, want 1", payload.CompanionSwarmCounts.Active)
	}
	if payload.CompanionSwarmCounts.Blocked != 2 {
		t.Fatalf("CompanionSwarmCounts.Blocked = %d, want 2", payload.CompanionSwarmCounts.Blocked)
	}
	if payload.CompanionSwarmCounts.Backlog < 1 {
		t.Fatalf("CompanionSwarmCounts.Backlog = %d, want backlog", payload.CompanionSwarmCounts.Backlog)
	}

	activeFound := false
	for _, swarm := range payload.CompanionSwarms {
		if swarm.ParentTaskKey != "status-swarm-active" {
			continue
		}
		activeFound = true
		if swarm.Status != "running" {
			t.Fatalf("active swarm status = %q, want running", swarm.Status)
		}
		if swarm.ActiveChildRunCount != 1 {
			t.Fatalf("active swarm active_child_run_count = %d, want 1", swarm.ActiveChildRunCount)
		}
		if swarm.BacklogCount != 0 {
			t.Fatalf("active swarm backlog_count = %d, want 0", swarm.BacklogCount)
		}
	}
	if !activeFound {
		t.Fatal("status-swarm-active missing from companion_swarms")
	}
}

func TestRunOverviewTextUsesCanonicalBoard(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"overview"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run(overview) error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"Attention",
		"Active Execution",
		"Workspace",
		"Initiatives",
		"alpha-cli title=Alpha",
		"odin-core title=Odin Core",
		"Work Items",
		"Run Attempts",
		"Companions",
		"Capability Catalog",
		"Approvals",
		"Observability",
		"Memory",
		"Intake Inbox",
		"Automation Triggers",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("overview output = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, "Processes") {
		t.Fatalf("overview output = %q, must not introduce Processes lane", output)
	}
}

func TestRunTUIOnceInvokesRunner(t *testing.T) {
	root := testRepoRoot(t)

	original := runTUI
	t.Cleanup(func() {
		runTUI = original
	})

	called := false
	runTUI = func(ctx context.Context, args []string, stdout io.Writer) error {
		called = true
		if len(args) != 1 || args[0] != "--once" {
			t.Fatalf("tui args = %v, want [--once]", args)
		}
		_, err := stdout.Write([]byte("tui invoked\n"))
		return err
	}

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"tui", "--once"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(tui --once) error = %v", err)
	}
	if !called {
		t.Fatal("Run(tui --once) did not invoke TUI runner")
	}
	if !strings.Contains(stdout.String(), "tui invoked") {
		t.Fatalf("stdout = %q, want TUI runner output", stdout.String())
	}
}

func TestRunTUIMissingPrometheusReturnsControlledError(t *testing.T) {
	root := testRepoRoot(t)

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{
		"tui",
		"--once",
		"--prometheus-url", "http://127.0.0.1:1",
		"--loki-url", "http://127.0.0.1:1",
	}, strings.NewReader(""), &stdout)
	if !errors.Is(err, tui.ErrUnavailableTelemetry) {
		t.Fatalf("Run(tui --once) error = %v, want ErrUnavailableTelemetry", err)
	}
	if strings.Contains(strings.ToUpper(stdout.String()), "HEALTHY") {
		t.Fatalf("stdout = %q, must not report healthy when Prometheus is missing", stdout.String())
	}
}

func TestRunOverviewJSONUsesCanonicalView(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"overview", "--json"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run(overview --json) error = %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("overview json = %v\n%s", err, stdout.String())
	}
	for _, key := range []string{"workspace", "initiatives", "capability_catalog", "intake_inbox", "automation_triggers"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("overview json keys = %v, want %q", raw, key)
		}
	}
	if _, ok := raw["Workspace"]; ok {
		t.Fatalf("overview json keys = %v, must use lower/snake keys", raw)
	}

	var payload struct {
		Workspace struct {
			Wiring          clioverview.Wiring `json:"wiring"`
			WorkspaceKey    string             `json:"workspace_key"`
			ControlScope    string             `json:"control_scope"`
			InitiativeCount int                `json:"initiative_count"`
		} `json:"workspace"`
		Initiatives []struct {
			InitiativeKey    string  `json:"initiative_key"`
			Title            string  `json:"title"`
			LinkedProjectKey *string `json:"linked_project_key"`
		} `json:"initiatives"`
		CapabilityCatalog struct {
			Wiring       clioverview.Wiring `json:"wiring"`
			CommandCount int                `json:"command_count"`
			ToolCount    int                `json:"tool_count"`
		} `json:"capability_catalog"`
		IntakeInbox struct {
			Wiring clioverview.Wiring `json:"wiring"`
		} `json:"intake_inbox"`
		AutomationTriggers struct {
			Wiring clioverview.Wiring `json:"wiring"`
		} `json:"automation_triggers"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(overview output) error = %v\n%s", err, stdout.String())
	}
	if payload.Workspace.Wiring != clioverview.WiringLive {
		t.Fatalf("Workspace.Wiring = %q, want %q", payload.Workspace.Wiring, clioverview.WiringLive)
	}
	if payload.Workspace.WorkspaceKey != "default" {
		t.Fatalf("Workspace.WorkspaceKey = %q, want default", payload.Workspace.WorkspaceKey)
	}
	if payload.Workspace.ControlScope != "global" {
		t.Fatalf("Workspace.ControlScope = %q, want global", payload.Workspace.ControlScope)
	}
	if payload.Workspace.InitiativeCount != 2 || len(payload.Initiatives) != 2 {
		t.Fatalf("overview initiatives = %d/%d, want registry-backed alpha-cli and odin-core", payload.Workspace.InitiativeCount, len(payload.Initiatives))
	}
	if payload.CapabilityCatalog.Wiring != clioverview.WiringCatalogBacked {
		t.Fatalf("CapabilityCatalog.Wiring = %q, want %q", payload.CapabilityCatalog.Wiring, clioverview.WiringCatalogBacked)
	}
	if payload.CapabilityCatalog.ToolCount == 0 {
		t.Fatalf("CapabilityCatalog = %+v, want populated builtin tool count", payload.CapabilityCatalog)
	}
	if payload.IntakeInbox.Wiring != clioverview.WiringLive {
		t.Fatalf("IntakeInbox.Wiring = %q, want %q", payload.IntakeInbox.Wiring, clioverview.WiringLive)
	}
	if payload.AutomationTriggers.Wiring != clioverview.WiringLive {
		t.Fatalf("AutomationTriggers.Wiring = %q, want %q", payload.AutomationTriggers.Wiring, clioverview.WiringLive)
	}
}

func TestRunIntakeRawCreateListShowDoesNotCreateTask(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"capture this raw request"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var createOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{
		"intake", "raw", "create",
		"--source", "operator",
		"--project", "odin-core",
		"--title", "Capture governed intake",
		"--type", "request",
		"--dedup-key", "governed-intake:1",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	}, strings.NewReader(""), &createOutput); err != nil {
		t.Fatalf("Run(intake raw create) error = %v", err)
	}
	if output := createOutput.String(); !strings.Contains(output, `"status": "received"`) || !strings.Contains(output, `"key": "intake-1"`) {
		t.Fatalf("create output = %s, want received intake-1", output)
	}

	var duplicateOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{
		"intake", "raw", "create",
		"--source", "operator",
		"--project", "odin-core",
		"--title", "Capture governed intake duplicate arrival",
		"--type", "request",
		"--dedup-key", "governed-intake:1",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	}, strings.NewReader(""), &duplicateOutput); err != nil {
		t.Fatalf("Run(duplicate intake raw create) error = %v", err)
	}
	if output := duplicateOutput.String(); !strings.Contains(output, `"status": "received"`) || !strings.Contains(output, `"key": "intake-2"`) {
		t.Fatalf("duplicate output = %s, want received intake-2", output)
	}

	var listOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "raw", "list", "--json"}, strings.NewReader(""), &listOutput); err != nil {
		t.Fatalf("Run(intake raw list) error = %v", err)
	}
	if output := listOutput.String(); !strings.Contains(output, `"requested_by": "codex"`) || !strings.Contains(output, `"payload_policy": "stored_in_source_facts_json"`) {
		t.Fatalf("list output = %s, want provenance and payload policy", output)
	}
	if output := listOutput.String(); strings.Count(output, `"dedup_key": "governed-intake:1"`) != 2 {
		t.Fatalf("list output = %s, want two raw duplicate arrivals", output)
	}

	var showOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "raw", "show", "intake-1", "--json"}, strings.NewReader(""), &showOutput); err != nil {
		t.Fatalf("Run(intake raw show) error = %v", err)
	}
	if output := showOutput.String(); !strings.Contains(output, `"dedup_key": "governed-intake:1"`) || !strings.Contains(output, `"payload"`) {
		t.Fatalf("show output = %s, want dedupe and payload visibility", output)
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	if output := jobsOutput.String(); !strings.Contains(output, `"jobs": []`) {
		t.Fatalf("jobs output = %s, want no jobs", output)
	}

	var workStatusOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &workStatusOutput); err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	if output := workStatusOutput.String(); !strings.Contains(output, "work_items=0") || !strings.Contains(output, "raw_intake_items=2") || !strings.Contains(output, "intake=raw_cli") {
		t.Fatalf("work status output = %s, want raw intake status without work items", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	if output := logsOutput.String(); strings.Count(output, `"type": "intake.item_created"`) != 2 || strings.Contains(output, `"type": "task.created"`) {
		t.Fatalf("logs output = %s, want intake event and no task event", output)
	}
}

func TestRunIntakeProcessCreatesReviewStatesWithoutExecution(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"prepare a careful ticket for review"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	createRaw := func(title, dedup string) {
		t.Helper()
		if err := Run(context.Background(), root, []string{
			"intake", "raw", "create",
			"--source", "operator",
			"--project", "odin-core",
			"--title", title,
			"--type", "request",
			"--dedup-key", dedup,
			"--requested-by", "codex",
			"--payload-file", payloadPath,
			"--json",
		}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake raw create %q) error = %v", title, err)
		}
	}
	createRaw("Build governed intake process review", "process-clear")
	createRaw("Help with this", "process-vague")
	createRaw("Build governed intake process review duplicate", "process-clear")

	var clearOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &clearOutput); err != nil {
		t.Fatalf("Run(intake process clear) error = %v", err)
	}
	if output := clearOutput.String(); !strings.Contains(output, `"status": "review_required"`) || !strings.Contains(output, `"routed_outcome": "draft_task"`) {
		t.Fatalf("clear process output = %s, want review_required draft_task", output)
	}

	var vagueOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-2", "--json"}, strings.NewReader(""), &vagueOutput); err != nil {
		t.Fatalf("Run(intake process vague) error = %v", err)
	}
	if output := vagueOutput.String(); !strings.Contains(output, `"status": "needs_clarification"`) || !strings.Contains(output, `"routed_outcome": "needs_clarification"`) {
		t.Fatalf("vague process output = %s, want needs_clarification", output)
	}

	var duplicateOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-3", "--json"}, strings.NewReader(""), &duplicateOutput); err != nil {
		t.Fatalf("Run(intake process duplicate) error = %v", err)
	}
	if output := duplicateOutput.String(); !strings.Contains(output, `"status": "duplicate_linked_or_suppressed"`) || !strings.Contains(output, `"canonical_intake_key": "intake-1"`) {
		t.Fatalf("duplicate process output = %s, want duplicate linked to intake-1", output)
	}

	var showOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "raw", "show", "intake-1", "--json"}, strings.NewReader(""), &showOutput); err != nil {
		t.Fatalf("Run(intake raw show processed) error = %v", err)
	}
	if output := showOutput.String(); !strings.Contains(output, `"processing"`) || !strings.Contains(output, `"draft_artifact"`) {
		t.Fatalf("show output = %s, want persisted processing artifact", output)
	}

	var overviewOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"overview", "--json"}, strings.NewReader(""), &overviewOutput); err != nil {
		t.Fatalf("Run(overview --json) error = %v", err)
	}
	if output := overviewOutput.String(); !strings.Contains(output, `"raw_item_count": 3`) || !strings.Contains(output, `"raw_processed_count": 3`) || !strings.Contains(output, `"open_work_item_count": 0`) {
		t.Fatalf("overview output = %s, want processed raw intake counts without work items", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "intake.processing_started"`,
		`"type": "intake.classified"`,
		`"type": "intake.dedupe_reviewed"`,
		`"type": "intake.routed"`,
		`"type": "intake.draft_artifact_created"`,
		`"type": "intake.clarification_needed"`,
		`"type": "intake.duplicate_linked_or_suppressed"`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}
	if strings.Contains(logsOutput.String(), `"type": "task.created"`) {
		t.Fatalf("logs output = %s, must not create task events", logsOutput.String())
	}

	for _, args := range [][]string{{"jobs", "--json"}, {"runs", "--json"}, {"approvals", "all", "--json"}} {
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v", args, err)
		}
		if !strings.Contains(output.String(), `[]`) {
			t.Fatalf("Run(%v) output = %s, want empty list", args, output.String())
		}
	}
}

func TestRunIntakeProcessDerivesTypeSpecificRoutingAndIntent(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"typed intake proof"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	createRaw := func(title, intakeType string) {
		t.Helper()
		if err := Run(context.Background(), root, []string{
			"intake", "raw", "create",
			"--source", "operator",
			"--project", "odin-core",
			"--title", title,
			"--type", intakeType,
			"--dedup-key", "typed-" + intakeType,
			"--requested-by", "codex",
			"--payload-file", payloadPath,
			"--json",
		}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake raw create %s) error = %v", intakeType, err)
		}
	}

	cases := []struct {
		title        string
		intakeType   string
		wantRoute    string
		wantArtifact string
		wantIntent   string
	}{
		{title: "Research release readiness constraints", intakeType: "research", wantRoute: "draft_research", wantArtifact: "draft_research", wantIntent: "read_only"},
		{title: "Draft operator release note", intakeType: "writing", wantRoute: "draft_document", wantArtifact: "draft_document", wantIntent: "mutation"},
		{title: "Organize project triage queue", intakeType: "admin", wantRoute: "draft_admin_task", wantArtifact: "draft_admin_task", wantIntent: "mutation"},
		{title: "Investigate import incident", intakeType: "bug", wantRoute: "draft_incident_review", wantArtifact: "draft_incident_review", wantIntent: "read_only"},
		{title: "Review approval boundary", intakeType: "governance", wantRoute: "draft_policy_change", wantArtifact: "draft_policy_change", wantIntent: "governance"},
		{title: "Clear cache artifact", intakeType: "destructive", wantRoute: "draft_destructive_action", wantArtifact: "draft_destructive_action", wantIntent: "destructive"},
	}

	for _, tc := range cases {
		createRaw(tc.title, tc.intakeType)
	}

	for i, tc := range cases {
		id := fmt.Sprintf("intake-%d", i+1)
		var output bytes.Buffer
		if err := Run(context.Background(), root, []string{"intake", "process", "--id", id, "--json"}, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(intake process %s) error = %v", id, err)
		}
		for _, want := range []string{
			`"intake_type": "` + tc.intakeType + `"`,
			`"routed_outcome": "` + tc.wantRoute + `"`,
			`"outcome": "` + tc.wantRoute + `"`,
			`"kind": "` + tc.wantArtifact + `"`,
			`"execution_intent": "` + tc.wantIntent + `"`,
			`"execution_intent_source": "intake_type:` + tc.intakeType + `"`,
		} {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("process output for %s = %s, want %s", tc.intakeType, output.String(), want)
			}
		}
	}
}

func TestRunIntakeProcessConvertsGoalLikeRawItemToGoal(t *testing.T) {
	root := testRepoRoot(t)

	var createOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{
		"intake", "raw", "create",
		"--text", "Build a browser executor for Odin research goals",
		"--json",
	}, strings.NewReader(""), &createOutput); err != nil {
		t.Fatalf("Run(intake raw create --text) error = %v", err)
	}
	if output := createOutput.String(); !strings.Contains(output, `"status": "received"`) || !strings.Contains(output, `"key": "intake-1"`) {
		t.Fatalf("create output = %s, want received intake-1", output)
	}

	var processOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &processOutput); err != nil {
		t.Fatalf("Run(intake process) error = %v", err)
	}
	if output := processOutput.String(); !strings.Contains(output, `"routed_outcome": "goal_created"`) || !strings.Contains(output, `"goal_id": 1`) {
		t.Fatalf("process output = %s, want goal conversion evidence", output)
	}
	if output := processOutput.String(); strings.Contains(output, `"status": "approved_for_execution"`) {
		t.Fatalf("process output = %s, must not auto-approve goal", output)
	}

	var rawShow bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "raw", "show", "intake-1", "--json"}, strings.NewReader(""), &rawShow); err != nil {
		t.Fatalf("Run(intake raw show) error = %v", err)
	}
	if output := rawShow.String(); !strings.Contains(output, `"goal_id": 1`) || !strings.Contains(output, `"processing"`) {
		t.Fatalf("raw show output = %s, want persisted intake goal link and processing evidence", output)
	}

	var goalList bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "list", "--json"}, strings.NewReader(""), &goalList); err != nil {
		t.Fatalf("Run(goal list) error = %v", err)
	}
	if output := goalList.String(); !strings.Contains(output, `"title": "Build a browser executor for Odin research goals"`) || !strings.Contains(output, `"status": "created"`) {
		t.Fatalf("goal list output = %s, want created converted goal", output)
	}

	var tickOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "tick", "--json"}, strings.NewReader(""), &tickOutput); err != nil {
		t.Fatalf("Run(goal tick) error = %v", err)
	}
	if output := tickOutput.String(); !strings.Contains(output, `"started": 0`) || strings.Contains(output, `"goal_run_id"`) {
		t.Fatalf("goal tick output = %s, want no execution without approval", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "intake.processed"`,
		`"type": "intake.routed_to_goal"`,
		`"type": "goal.created"`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}
}

func TestRunIntakeProcessConvertsProjectLikeRawItemToGoal(t *testing.T) {
	root := testRepoRoot(t)

	var createOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{
		"intake", "raw", "create",
		"--text", "Plan the Odin project for browser session handoff",
		"--json",
	}, strings.NewReader(""), &createOutput); err != nil {
		t.Fatalf("Run(intake raw create --text) error = %v", err)
	}
	if output := createOutput.String(); !strings.Contains(output, `"status": "received"`) || !strings.Contains(output, `"key": "intake-1"`) {
		t.Fatalf("create output = %s, want received intake-1", output)
	}

	var processOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &processOutput); err != nil {
		t.Fatalf("Run(intake process) error = %v", err)
	}
	if output := processOutput.String(); !strings.Contains(output, `"routed_outcome": "goal_created"`) || !strings.Contains(output, `"goal_id": 1`) {
		t.Fatalf("process output = %s, want project-like intake routed to goal", output)
	}

	var goalShow bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "show", "--id", "1", "--json"}, strings.NewReader(""), &goalShow); err != nil {
		t.Fatalf("Run(goal show) error = %v", err)
	}
	if output := goalShow.String(); !strings.Contains(output, `"status": "created"`) || strings.Contains(output, `"status": "approved_for_execution"`) {
		t.Fatalf("goal show output = %s, want created unapproved goal", output)
	}
}

func TestRunIntakeProcessDuplicateGoalLikeRawItemDoesNotCreateSecondGoal(t *testing.T) {
	root := testRepoRoot(t)

	createRaw := func() {
		t.Helper()
		if err := Run(context.Background(), root, []string{
			"intake", "raw", "create",
			"--text", "Build a browser executor for Odin research goals",
			"--json",
		}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake raw create --text) error = %v", err)
		}
	}
	createRaw()
	createRaw()

	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake process first) error = %v", err)
	}
	var duplicateOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-2", "--json"}, strings.NewReader(""), &duplicateOutput); err != nil {
		t.Fatalf("Run(intake process duplicate) error = %v", err)
	}
	if output := duplicateOutput.String(); !strings.Contains(output, `"status": "duplicate_linked_or_suppressed"`) || !strings.Contains(output, `"canonical_intake_key": "intake-1"`) {
		t.Fatalf("duplicate output = %s, want duplicate linked to first intake", output)
	}

	var goalList bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "list", "--json"}, strings.NewReader(""), &goalList); err != nil {
		t.Fatalf("Run(goal list) error = %v", err)
	}
	if output := goalList.String(); strings.Count(output, `"title": "Build a browser executor for Odin research goals"`) != 1 {
		t.Fatalf("goal list output = %s, want exactly one converted goal", output)
	}
}

func TestRunIntakeReviewPromotesOnlyOnOperatorAccept(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"prepare a careful ticket for review"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	createRaw := func(title, dedup string) {
		t.Helper()
		if err := Run(context.Background(), root, []string{
			"intake", "raw", "create",
			"--source", "operator",
			"--project", "odin-core",
			"--title", title,
			"--type", "request",
			"--dedup-key", dedup,
			"--requested-by", "codex",
			"--payload-file", payloadPath,
			"--json",
		}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake raw create %q) error = %v", title, err)
		}
	}
	createRaw("Build governed intake review queue", "review-clear")
	createRaw("Help with this", "review-vague")
	createRaw("Build governed intake review queue duplicate", "review-clear")
	createRaw("Archive governed intake review queue item", "review-archive")

	for _, id := range []string{"intake-1", "intake-2", "intake-3", "intake-4"} {
		if err := Run(context.Background(), root, []string{"intake", "process", "--id", id, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake process %s) error = %v", id, err)
		}
	}

	var listOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "list", "--json"}, strings.NewReader(""), &listOutput); err != nil {
		t.Fatalf("Run(intake review list) error = %v", err)
	}
	if output := listOutput.String(); !strings.Contains(output, `"status": "review_required"`) || !strings.Contains(output, `"status": "needs_clarification"`) || !strings.Contains(output, `"status": "duplicate_linked_or_suppressed"`) {
		t.Fatalf("review list output = %s, want all reviewable states", output)
	}

	var showOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "show", "intake-1", "--json"}, strings.NewReader(""), &showOutput); err != nil {
		t.Fatalf("Run(intake review show) error = %v", err)
	}
	if output := showOutput.String(); !strings.Contains(output, `"draft_artifact"`) || !strings.Contains(output, `"review_state": "review_required"`) {
		t.Fatalf("review show output = %s, want draft artifact", output)
	}

	var acceptOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "accept", "intake-1", "--json"}, strings.NewReader(""), &acceptOutput); err != nil {
		t.Fatalf("Run(intake review accept) error = %v", err)
	}
	if output := acceptOutput.String(); !strings.Contains(output, `"decision": "accepted"`) || !strings.Contains(output, `"work_created": true`) || !strings.Contains(output, `"work_item"`) {
		t.Fatalf("accept output = %s, want accepted work item", output)
	}
	var firstAccept struct {
		WorkItem struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		} `json:"work_item"`
	}
	if err := json.Unmarshal(acceptOutput.Bytes(), &firstAccept); err != nil {
		t.Fatalf("json.Unmarshal(first accept) error = %v", err)
	}
	if firstAccept.WorkItem.ID == 0 || firstAccept.WorkItem.Key == "" {
		t.Fatalf("first accept work item = %+v, want durable work identity", firstAccept.WorkItem)
	}

	var repeatAcceptOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "accept", "intake-1", "--json"}, strings.NewReader(""), &repeatAcceptOutput); err != nil {
		t.Fatalf("Run(intake review accept repeat) error = %v", err)
	}
	var repeatAccept struct {
		Decision    string `json:"decision"`
		WorkCreated bool   `json:"work_created"`
		WorkItem    struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		} `json:"work_item"`
	}
	if err := json.Unmarshal(repeatAcceptOutput.Bytes(), &repeatAccept); err != nil {
		t.Fatalf("json.Unmarshal(repeat accept) error = %v", err)
	}
	if repeatAccept.Decision != "accepted" || repeatAccept.WorkCreated {
		t.Fatalf("repeat accept = %+v, want accepted with existing work item", repeatAccept)
	}
	if repeatAccept.WorkItem.ID != firstAccept.WorkItem.ID || repeatAccept.WorkItem.Key != firstAccept.WorkItem.Key {
		t.Fatalf("repeat accept work item = %+v, want original %+v", repeatAccept.WorkItem, firstAccept.WorkItem)
	}

	var acceptedShowOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "show", "intake-1", "--json"}, strings.NewReader(""), &acceptedShowOutput); err != nil {
		t.Fatalf("Run(intake review show accepted) error = %v", err)
	}
	if output := acceptedShowOutput.String(); !strings.Contains(output, `"accepted_work_item_id":`) || !strings.Contains(output, `"accepted_work_item_key":`) {
		t.Fatalf("accepted show output = %s, want durable accepted work link", output)
	}

	var rejectOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "reject", "intake-2", "--json"}, strings.NewReader(""), &rejectOutput); err != nil {
		t.Fatalf("Run(intake review reject) error = %v", err)
	}
	if output := rejectOutput.String(); !strings.Contains(output, `"decision": "rejected"`) || !strings.Contains(output, `"work_created": false`) {
		t.Fatalf("reject output = %s, want rejected without work", output)
	}

	var clarifyOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "clarify", "intake-2", "--json"}, strings.NewReader(""), &clarifyOutput); err != nil {
		t.Fatalf("Run(intake review clarify) error = %v", err)
	}
	if output := clarifyOutput.String(); !strings.Contains(output, `"decision": "clarification_requested"`) || !strings.Contains(output, `"status": "needs_clarification"`) {
		t.Fatalf("clarify output = %s, want clarification state", output)
	}

	var duplicateAcceptOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "accept", "intake-3", "--json"}, strings.NewReader(""), &duplicateAcceptOutput); err != nil {
		t.Fatalf("Run(intake review accept duplicate) error = %v", err)
	}
	if output := duplicateAcceptOutput.String(); !strings.Contains(output, `"decision": "duplicate_acknowledged"`) || !strings.Contains(output, `"work_created": false`) {
		t.Fatalf("duplicate accept output = %s, want duplicate acknowledged without work", output)
	}

	var archiveOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "archive", "intake-4", "--json"}, strings.NewReader(""), &archiveOutput); err != nil {
		t.Fatalf("Run(intake review archive) error = %v", err)
	}
	if output := archiveOutput.String(); !strings.Contains(output, `"decision": "archived"`) || !strings.Contains(output, `"work_created": false`) {
		t.Fatalf("archive output = %s, want archived without work", output)
	}

	var workStatusOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &workStatusOutput); err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	if output := workStatusOutput.String(); !strings.Contains(output, "work_items=1") || !strings.Contains(output, "intake_review_items=") {
		t.Fatalf("work status output = %s, want one work item and review queue count", output)
	}

	var overviewOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"overview", "--json"}, strings.NewReader(""), &overviewOutput); err != nil {
		t.Fatalf("Run(overview --json) error = %v", err)
	}
	if output := overviewOutput.String(); !strings.Contains(output, `"open_work_item_count": 1`) || !strings.Contains(output, `"review_queue_count":`) {
		t.Fatalf("overview output = %s, want promoted work item and review queue count", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "intake.review_accepted"`,
		`"type": "intake.review_rejected"`,
		`"type": "intake.review_clarification_requested"`,
		`"type": "intake.review_duplicate_acknowledged"`,
		`"type": "intake.review_archived"`,
		`"type": "task.created"`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}
	if output := logsOutput.String(); !strings.Contains(output, `"work_item_id":`) || !strings.Contains(output, `"work_item_key":`) {
		t.Fatalf("logs output = %s, want accepted audit payload with linked work identity", output)
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	if output := jobsOutput.String(); !strings.Contains(output, `"jobs"`) || !strings.Contains(output, `"status": "queued"`) {
		t.Fatalf("jobs output = %s, want accepted work item visible as queued job", output)
	}
	if count := strings.Count(jobsOutput.String(), `"status": "queued"`); count != 1 {
		t.Fatalf("jobs output = %s, want exactly one queued job after repeat accept", jobsOutput.String())
	}

	for _, args := range [][]string{{"runs", "--json"}, {"approvals", "all", "--json"}} {
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v", args, err)
		}
		if !strings.Contains(output.String(), `[]`) {
			t.Fatalf("Run(%v) output = %s, want empty list", args, output.String())
		}
	}
}

func TestRunIntakeReviewAcceptRequiresApprovalForRiskyIntake(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"policy sensitive request"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	createRaw := func(title, dedup string) {
		t.Helper()
		if err := Run(context.Background(), root, []string{
			"intake", "raw", "create",
			"--source", "operator",
			"--project", "odin-core",
			"--title", title,
			"--type", "request",
			"--dedup-key", dedup,
			"--requested-by", "codex",
			"--payload-file", payloadPath,
			"--json",
		}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake raw create %q) error = %v", title, err)
		}
	}
	createRaw("Build low risk intake work", "approval-low-risk")
	createRaw("Delete production data from risky system", "approval-risky")

	for _, id := range []string{"intake-1", "intake-2"} {
		if err := Run(context.Background(), root, []string{"intake", "process", "--id", id, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake process %s) error = %v", id, err)
		}
	}

	var lowRiskAccept bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "accept", "intake-1", "--json"}, strings.NewReader(""), &lowRiskAccept); err != nil {
		t.Fatalf("Run(low-risk accept) error = %v", err)
	}
	if output := lowRiskAccept.String(); !strings.Contains(output, `"work_created": true`) || strings.Contains(output, `"approval_required": true`) {
		t.Fatalf("low-risk accept output = %s, want direct work creation", output)
	}

	var riskyAccept bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "accept", "intake-2", "--json"}, strings.NewReader(""), &riskyAccept); err != nil {
		t.Fatalf("Run(risky accept) error = %v", err)
	}
	if output := riskyAccept.String(); !strings.Contains(output, `"decision": "approval_required"`) || !strings.Contains(output, `"work_created": false`) || !strings.Contains(output, `"approval_required": true`) {
		t.Fatalf("risky accept output = %s, want approval required without work", output)
	}
	if output := riskyAccept.String(); !strings.Contains(output, `"policy_reason": "risky_intake_requires_operator_approval"`) {
		t.Fatalf("risky accept output = %s, want policy reason", output)
	}

	var repeatRiskyAccept bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "accept", "intake-2", "--json"}, strings.NewReader(""), &repeatRiskyAccept); err != nil {
		t.Fatalf("Run(risky repeat accept) error = %v", err)
	}
	if output := repeatRiskyAccept.String(); !strings.Contains(output, `"decision": "approval_required"`) || !strings.Contains(output, `"work_created": false`) || strings.Contains(output, `"work_item"`) {
		t.Fatalf("risky repeat accept output = %s, want idempotent approval block without work", output)
	}

	var showOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "review", "show", "intake-2", "--json"}, strings.NewReader(""), &showOutput); err != nil {
		t.Fatalf("Run(risky review show) error = %v", err)
	}
	if output := showOutput.String(); !strings.Contains(output, `"status": "approval_required"`) || !strings.Contains(output, `"blocked_pending_approval": true`) {
		t.Fatalf("risky show output = %s, want blocked approval state", output)
	}

	var approvalsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"approvals", "all", "--json"}, strings.NewReader(""), &approvalsOutput); err != nil {
		t.Fatalf("Run(approvals all --json) error = %v", err)
	}
	if output := approvalsOutput.String(); !strings.Contains(output, `"approvals": []`) {
		t.Fatalf("approvals output = %s, want no task-backed approvals before work creation", output)
	}

	var overviewOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"overview", "--json"}, strings.NewReader(""), &overviewOutput); err != nil {
		t.Fatalf("Run(overview --json) error = %v", err)
	}
	if output := overviewOutput.String(); !strings.Contains(output, `"open_work_item_count": 1`) || !strings.Contains(output, `"intake_approval_required_count": 1`) {
		t.Fatalf("overview output = %s, want one direct work item and one intake approval block", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	if output := logsOutput.String(); !strings.Contains(output, `"type": "intake.review_approval_required"`) || !strings.Contains(output, `"policy_reason": "risky_intake_requires_operator_approval"`) {
		t.Fatalf("logs output = %s, want approval policy audit evidence", output)
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	if count := strings.Count(jobsOutput.String(), `"status": "queued"`); count != 1 {
		t.Fatalf("jobs output = %s, want only low-risk queued work item", jobsOutput.String())
	}

	for _, args := range [][]string{{"runs", "--json"}, {"approvals", "all", "--json"}} {
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v", args, err)
		}
		if !strings.Contains(output.String(), `[]`) {
			t.Fatalf("Run(%v) output = %s, want empty list", args, output.String())
		}
	}

	var workStatusOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &workStatusOutput); err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	if output := workStatusOutput.String(); !strings.Contains(output, "work_items=1") || !strings.Contains(output, "intake_approval_required_items=1") {
		t.Fatalf("work status output = %s, want approval-required intake count", output)
	}
}

func TestRunIntakePromotionPersistsDerivedGovernanceIntent(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "intake governance intent proof")
	run(
		"intake", "raw", "create",
		"--source", "operator",
		"--project", testProjectKey,
		"--title", "Review approval boundary",
		"--type", "governance",
		"--dedup-key", "governance-intent",
		"--requested-by", "codex",
		"--json",
	)
	processOutput := run("intake", "process", "--id", "intake-1", "--json")
	for _, want := range []string{
		`"routed_outcome": "draft_policy_change"`,
		`"execution_intent": "governance"`,
		`"execution_intent_source": "intake_type:governance"`,
	} {
		if !strings.Contains(processOutput, want) {
			t.Fatalf("process output = %s, want %s", processOutput, want)
		}
	}

	reviewOutput := run("intake", "review", "accept", "intake-1", "--json")
	if !strings.Contains(reviewOutput, `"approval_required": true`) || !strings.Contains(reviewOutput, `"policy_reason": "intake_intent_requires_operator_approval"`) {
		t.Fatalf("review output = %s, want approval required from intake-derived governance intent", reviewOutput)
	}
	approveOutput := run("intake", "approval", "approve", "intake-1", "--json")
	if !strings.Contains(approveOutput, `"work_item"`) || !strings.Contains(approveOutput, `"key": "intake-review-1"`) {
		t.Fatalf("approve output = %s, want linked work item", approveOutput)
	}

	jobsOutput := run("jobs", "--json")
	for _, want := range []string{
		`"task_key": "intake-review-1"`,
		`"execution_intent": "governance"`,
		`"execution_intent_source": "intake_type:governance"`,
	} {
		if !strings.Contains(jobsOutput, want) {
			t.Fatalf("jobs output = %s, want %s", jobsOutput, want)
		}
	}

	dispatchOutput := run("work", "dispatch", "--task", "intake-review-1", "--json")
	for _, want := range []string{
		`"dispatched": false`,
		`"reason": "approval_required"`,
		`"status": "blocked"`,
		`"execution_intent": "governance"`,
		`"execution_intent_source": "intake_type:governance"`,
	} {
		if !strings.Contains(dispatchOutput, want) {
			t.Fatalf("dispatch output = %s, want %s", dispatchOutput, want)
		}
	}

	logsOutput := run("logs", "--json")
	for _, want := range []string{
		`"type": "intake.review_approval_required"`,
		`"type": "task.created"`,
		`"execution_intent": "governance"`,
		`"execution_intent_source": "intake_type:governance"`,
	} {
		if !strings.Contains(logsOutput, want) {
			t.Fatalf("logs output = %s, want %s", logsOutput, want)
		}
	}
}

func TestRunIntakeApprovalResolutionPromotesOrDeniesRiskyIntake(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"risky production request"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	createRisky := func(title, dedup string) {
		t.Helper()
		if err := Run(context.Background(), root, []string{
			"intake", "raw", "create",
			"--source", "operator",
			"--project", "odin-core",
			"--title", title,
			"--type", "request",
			"--dedup-key", dedup,
			"--requested-by", "codex",
			"--payload-file", payloadPath,
			"--json",
		}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake raw create %q) error = %v", title, err)
		}
	}
	createRisky("Delete production cache after review", "approval-approve")
	createRisky("Delete production archive after review", "approval-deny")

	for _, id := range []string{"intake-1", "intake-2"} {
		if err := Run(context.Background(), root, []string{"intake", "process", "--id", id, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake process %s) error = %v", id, err)
		}
		if err := Run(context.Background(), root, []string{"intake", "review", "accept", id, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(intake review accept %s) error = %v", id, err)
		}
	}

	var listOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "approval", "list", "--json"}, strings.NewReader(""), &listOutput); err != nil {
		t.Fatalf("Run(intake approval list) error = %v", err)
	}
	if output := listOutput.String(); strings.Count(output, `"status": "approval_required"`) != 2 {
		t.Fatalf("approval list output = %s, want two approval-required items", output)
	}

	var showOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "approval", "show", "intake-1", "--json"}, strings.NewReader(""), &showOutput); err != nil {
		t.Fatalf("Run(intake approval show) error = %v", err)
	}
	if output := showOutput.String(); !strings.Contains(output, `"blocked_pending_approval": true`) || !strings.Contains(output, `"policy_reason": "risky_intake_requires_operator_approval"`) {
		t.Fatalf("approval show output = %s, want pending policy block", output)
	}

	var approveOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "approval", "approve", "intake-1", "--json"}, strings.NewReader(""), &approveOutput); err != nil {
		t.Fatalf("Run(intake approval approve) error = %v", err)
	}
	var approved struct {
		Decision    string `json:"decision"`
		WorkCreated bool   `json:"work_created"`
		WorkItem    struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		} `json:"work_item"`
	}
	if err := json.Unmarshal(approveOutput.Bytes(), &approved); err != nil {
		t.Fatalf("json.Unmarshal(approve) error = %v", err)
	}
	if approved.Decision != "approved" || !approved.WorkCreated || approved.WorkItem.ID == 0 || approved.WorkItem.Key == "" {
		t.Fatalf("approve output = %+v, want approved work creation", approved)
	}

	var repeatApproveOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "approval", "approve", "intake-1", "--json"}, strings.NewReader(""), &repeatApproveOutput); err != nil {
		t.Fatalf("Run(intake approval approve repeat) error = %v", err)
	}
	var repeatApproved struct {
		Decision    string `json:"decision"`
		WorkCreated bool   `json:"work_created"`
		WorkItem    struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		} `json:"work_item"`
	}
	if err := json.Unmarshal(repeatApproveOutput.Bytes(), &repeatApproved); err != nil {
		t.Fatalf("json.Unmarshal(repeat approve) error = %v", err)
	}
	if repeatApproved.Decision != "approved" || repeatApproved.WorkCreated || repeatApproved.WorkItem.ID != approved.WorkItem.ID || repeatApproved.WorkItem.Key != approved.WorkItem.Key {
		t.Fatalf("repeat approve = %+v, want original work item without duplicate creation", repeatApproved)
	}

	var denyOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "approval", "deny", "intake-2", "--json"}, strings.NewReader(""), &denyOutput); err != nil {
		t.Fatalf("Run(intake approval deny) error = %v", err)
	}
	if output := denyOutput.String(); !strings.Contains(output, `"decision": "denied"`) || !strings.Contains(output, `"work_created": false`) || strings.Contains(output, `"work_item"`) {
		t.Fatalf("deny output = %s, want denied without work", output)
	}

	var repeatDenyOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"intake", "approval", "deny", "intake-2", "--json"}, strings.NewReader(""), &repeatDenyOutput); err != nil {
		t.Fatalf("Run(intake approval deny repeat) error = %v", err)
	}
	if output := repeatDenyOutput.String(); !strings.Contains(output, `"decision": "denied"`) || !strings.Contains(output, `"work_created": false`) || strings.Contains(output, `"work_item"`) {
		t.Fatalf("repeat deny output = %s, want safe denied state without work", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "intake.approval_approved"`,
		`"type": "intake.approval_denied"`,
		`"work_item_key":`,
		`"policy_reason": "operator_approved_risky_intake"`,
		`"policy_reason": "operator_denied_risky_intake"`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	if count := strings.Count(jobsOutput.String(), `"status": "queued"`); count != 1 {
		t.Fatalf("jobs output = %s, want exactly one queued job from approved risky intake", jobsOutput.String())
	}

	for _, args := range [][]string{{"runs", "--json"}, {"approvals", "all", "--json"}} {
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v", args, err)
		}
		if !strings.Contains(output.String(), `[]`) {
			t.Fatalf("Run(%v) output = %s, want empty list", args, output.String())
		}
	}

	var workStatusOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &workStatusOutput); err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	if output := workStatusOutput.String(); !strings.Contains(output, "work_items=1") || !strings.Contains(output, "intake_approval_required_items=0") {
		t.Fatalf("work status output = %s, want one work item and no pending intake approvals", output)
	}
}

func TestRunUnifiedReviewQueueListsShowsAndRoutesExistingReviewObjects(t *testing.T) {
	configureLifecycleHarnessDriver(t)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"unified review proof"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	seedReviewableSkill(t, root, "review-queue-skill", "review queue skill ready", `{"title":"Queue skill artifact","next_step":"review"}`)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}
	createRaw := func(title, dedup string) {
		t.Helper()
		run(
			"intake", "raw", "create",
			"--source", "operator",
			"--project", testProjectKey,
			"--title", title,
			"--type", "request",
			"--dedup-key", dedup,
			"--requested-by", "codex",
			"--payload-file", payloadPath,
			"--json",
		)
	}

	createRaw("Build unified review queue proof", "unified-review-clear")
	createRaw("Delete production data through unified review", "unified-review-risky")
	run("intake", "process", "--id", "intake-1", "--json")
	run("intake", "process", "--id", "intake-2", "--json")
	run("intake", "review", "accept", "intake-2", "--json")

	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "unified review approval proof")
	run("companion", "run", "primary", "--objective", "Prepare unified review task-backed approval", "--trigger", "test", "--json")
	blocked := run("work", "dispatch", "--task", "1", "--json")
	if !strings.Contains(blocked, `"reason": "approval_required"`) {
		t.Fatalf("dispatch output = %s, want task-backed approval", blocked)
	}

	run("project", "select", testProjectKey)
	invoked := run("skills", "invoke", "review-queue-skill", "--json")
	if !strings.Contains(invoked, `"runtime_effect": "durable_reviewable_artifact"`) {
		t.Fatalf("skills invoke output = %s, want reviewable artifact", invoked)
	}

	list := run("review", "list", "--json")
	for _, want := range []string{
		`"queue_id": "intake-review:1"`,
		`"source_type": "intake_review"`,
		`"queue_id": "intake-approval:2"`,
		`"source_type": "intake_approval"`,
		`"queue_id": "approval:1"`,
		`"source_type": "task_approval"`,
		`"queue_id": "skill-artifact:1"`,
		`"source_type": "skill_artifact"`,
		`"allowed_actions": [`,
	} {
		if !strings.Contains(list, want) {
			t.Fatalf("review list output = %s, want %s", list, want)
		}
	}
	var mixedList struct {
		Items []struct {
			QueueID        string   `json:"queue_id"`
			Type           string   `json:"type"`
			SourceType     string   `json:"source_type"`
			Source         string   `json:"source"`
			Status         string   `json:"status"`
			Reason         string   `json:"reason"`
			CreatedAt      string   `json:"created_at"`
			Risk           string   `json:"risk"`
			AllowedActions []string `json:"allowed_actions"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(list), &mixedList); err != nil {
		t.Fatalf("json.Unmarshal(review list) error = %v; output=%s", err, list)
	}
	for _, item := range mixedList.Items {
		if item.QueueID == "" || item.Type == "" || item.SourceType == "" || item.Source == "" || item.Status == "" || item.Reason == "" || item.CreatedAt == "" || item.Risk == "" || item.AllowedActions == nil {
			t.Fatalf("review list item = %+v, want type/source/status/created/reason/risk/actions", item)
		}
	}

	show := run("review", "show", "intake-review:1", "--json")
	if !strings.Contains(show, `"source_type": "intake_review"`) || !strings.Contains(show, `"review_state": "review_required"`) {
		t.Fatalf("review show output = %s, want intake review detail", show)
	}

	accepted := run("review", "act", "intake-review:1", "accept", "--json")
	if !strings.Contains(accepted, `"decision": "accepted"`) || !strings.Contains(accepted, `"work_created": true`) {
		t.Fatalf("review act accept output = %s, want accepted intake work", accepted)
	}
	deniedIntake := run("review", "act", "intake-approval:2", "deny", "--json")
	if !strings.Contains(deniedIntake, `"decision": "denied"`) || !strings.Contains(deniedIntake, `"work_created": false`) {
		t.Fatalf("review act intake deny output = %s, want denied intake approval", deniedIntake)
	}
	deniedApproval := run("review", "act", "approval:1", "deny", "--json")
	if !strings.Contains(deniedApproval, `"status": "denied"`) || !strings.Contains(deniedApproval, `"result": "denied"`) {
		t.Fatalf("review act approval deny output = %s, want denied task approval", deniedApproval)
	}
	archivedArtifact := run("review", "act", "skill-artifact:1", "archive", "--json")
	if !strings.Contains(archivedArtifact, `"decision": "archived"`) || !strings.Contains(archivedArtifact, `"work_created": false`) {
		t.Fatalf("review act artifact archive output = %s, want archived skill artifact", archivedArtifact)
	}

	logsOutput := run("logs", "--json")
	for _, want := range []string{
		`"type": "intake.review_accepted"`,
		`"type": "intake.approval_denied"`,
		`"type": "skill.artifact_reviewed"`,
	} {
		if !strings.Contains(logsOutput, want) {
			t.Fatalf("logs output = %s, want %s", logsOutput, want)
		}
	}
	run("project", "select", "odin-core")
	approvalLogsOutput := run("logs", "--json")
	if !strings.Contains(approvalLogsOutput, `"type": "approval.resolved"`) {
		t.Fatalf("approval logs output = %s, want task approval resolution event", approvalLogsOutput)
	}

	run("project", "select", testProjectKey)
	overview := run("overview", "--json")
	for _, want := range []string{
		`"open_work_item_count": 1`,
		`"archived_artifact_count": 1`,
	} {
		if !strings.Contains(overview, want) {
			t.Fatalf("overview output = %s, want %s", overview, want)
		}
	}
}

func TestRunApprovalGatedReviewHumanOutputExplainsOperatorAction(t *testing.T) {
	configureLifecycleHarnessDriver(t)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "human approval UX proof")
	run("companion", "run", "primary", "--objective", "Prepare approval UX proof", "--trigger", "test", "--json")
	blocked := run("work", "dispatch", "--task", "1", "--json")
	if !strings.Contains(blocked, `"reason": "approval_required"`) {
		t.Fatalf("dispatch output = %s, want approval gate", blocked)
	}

	approvalList := run("approvals", "all")
	for _, want := range []string{
		"approval=1",
		"source=approval_requests",
		"risk=governance",
		"reason=approval_required",
		"task=prepare-approval-ux-proof-",
		"status=pending",
		"resolver=supported",
		"actions=approve,deny",
		"next_steps=inspect with odin approvals show 1; resolve with odin approvals resolve 1 <approve|deny> <reason...>",
		"on_approve=task unblocked or registered continuation starts",
	} {
		if !strings.Contains(approvalList, want) {
			t.Fatalf("approvals output = %q, want %q", approvalList, want)
		}
	}

	approvalShow := run("approvals", "show", "1")
	for _, want := range []string{
		"approval=1",
		"source=approval_requests",
		"risk=governance",
		"reason=approval_required",
		"task_status=blocked",
		"actions=approve,deny",
		"next_steps=inspect with odin approvals show 1; resolve with odin approvals resolve 1 <approve|deny> <reason...>",
		"on_approve=task unblocked or registered continuation starts",
	} {
		if !strings.Contains(approvalShow, want) {
			t.Fatalf("approvals show output = %q, want %q", approvalShow, want)
		}
	}

	reviewList := run("review", "list")
	for _, want := range []string{
		"review=approval:1",
		"type=task_approval",
		"source=approval_requests",
		"risk=governance",
		"reason=task_approval_pending",
		"status=pending",
		"actions=approve,deny",
		"next_steps=inspect with odin review show approval:1; act with odin review act approval:1 <approve|deny>",
	} {
		if !strings.Contains(reviewList, want) {
			t.Fatalf("review list output = %q, want %q", reviewList, want)
		}
	}

	reviewShow := run("review", "show", "approval:1")
	for _, want := range []string{
		"review=approval:1",
		"type=task_approval",
		"source=approval_requests",
		"risk=governance",
		"reason=task_approval_pending",
		"status=pending",
		"actions=approve,deny",
		"next_steps=inspect with odin review show approval:1; act with odin review act approval:1 <approve|deny>",
	} {
		if !strings.Contains(reviewShow, want) {
			t.Fatalf("review show output = %q, want %q", reviewShow, want)
		}
	}

	approved := run("review", "act", "approval:1", "approve")
	for _, want := range []string{
		"approval=1",
		"status=resolved",
		"result=approved",
		"summary=approval granted; task unblocked",
	} {
		if !strings.Contains(approved, want) {
			t.Fatalf("review act output = %q, want %q", approved, want)
		}
	}

	logs := run("logs", "--json")
	for _, want := range []string{
		`"type": "approval.resolved"`,
		`"task_id": 1`,
		`"status": "approved"`,
		`"decision_by": "operator"`,
		`"reason": "unified review decision"`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want %s", logs, want)
		}
	}
}

func TestRunUnifiedReviewQueueSurfacesMemoryProposalsReadOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	project, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
		Key:  testProjectKey,
		Name: "Alpha CLI",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	recorded, err := store.RecordMemorySummary(context.Background(), sqlite.RecordMemorySummaryParams{
		ProjectID:   &project.ID,
		Scope:       "project",
		ScopeKey:    testProjectKey,
		MemoryType:  "social_draft",
		Summary:     "Draft social memory awaiting operator review",
		DetailsJSON: `{"fields":{"approval":"pending","channel":"x","content_kind":"post"}}`,
	})
	if err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}
	store.Close()

	list := run("review", "list", "--json")
	for _, want := range []string{
		`"queue_id": "memory-proposal:` + int64String(recorded.ID) + `"`,
		`"source_type": "memory_proposal"`,
		`"source": "memory_summaries"`,
		`"risk": "governance"`,
		`"allowed_actions": []`,
	} {
		if !strings.Contains(list, want) {
			t.Fatalf("review list output = %s, want %s", list, want)
		}
	}

	show := run("review", "show", "memory-proposal:"+int64String(recorded.ID), "--json")
	for _, want := range []string{
		`"source_type": "memory_proposal"`,
		`"memory_type": "social_draft"`,
		`"approval": "pending"`,
		`"risk": "governance"`,
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("review show output = %s, want %s", show, want)
		}
	}

	var actionOutput bytes.Buffer
	err = Run(context.Background(), root, []string{"review", "act", "memory-proposal:" + int64String(recorded.ID), "approve", "--json"}, strings.NewReader(""), &actionOutput)
	if err == nil || !strings.Contains(err.Error(), "memory proposal review actions are not implemented") {
		t.Fatalf("Run(review act memory proposal approve) error = %v output=%s, want forbidden action", err, actionOutput.String())
	}
}

func TestRunReviewQueueIncludesGoalReviewItems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}
	createGoal := func(title string) int64 {
		t.Helper()
		output := run("goal", "create", "--title", title, "--json")
		created := decodeGoalEnvelope(t, []byte(output))
		return created.ID
	}

	run("intake", "raw", "create", "--text", "Build a browser executor for Odin research goals", "--json")
	run("intake", "process", "--id", "intake-1", "--json")
	manualCreatedID := createGoal("Manual created goal review")
	plannedID := createGoal("Planned goal awaiting approval")
	run("goal", "transition", "--id", int64String(plannedID), "--status", "planned", "--json")
	blockedID := createGoal("Blocked goal needs human action")
	run("goal", "transition", "--id", int64String(blockedID), "--status", "planned", "--json")
	run("goal", "transition", "--id", int64String(blockedID), "--status", "approved_for_execution", "--json")
	run("goal", "tick", "--json")
	run("goal", "tick", "--json")

	list := run("review", "list", "--json")
	for _, want := range []string{
		`"review_id": "intake-goal:1"`,
		`"source_type": "intake_goal_conversion"`,
		`"goal_id": 1`,
		`"review_id": "goal:` + int64String(manualCreatedID) + `"`,
		`"source_type": "goal"`,
		`"title": "Manual created goal review"`,
		`"review_id": "goal-approval:` + int64String(plannedID) + `"`,
		`"reason": "goal_planned_awaiting_approval"`,
		`"source_type": "goal_blocker"`,
		`"title": "Blocked goal needs human action"`,
	} {
		if !strings.Contains(list, want) {
			t.Fatalf("review list output = %s, want %s", list, want)
		}
	}
	if count := strings.Count(list, `"title": "Build a browser executor for Odin research goals"`); count != 1 {
		t.Fatalf("review list output = %s, want converted intake goal title once, got %d", list, count)
	}

	var listed struct {
		Items []struct {
			ReviewID   string `json:"review_id"`
			SourceType string `json:"source_type"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(list), &listed); err != nil {
		t.Fatalf("review list json decode error = %v; output=%s", err, list)
	}
	var convertedReviewID string
	for _, item := range listed.Items {
		if item.SourceType == "intake_goal_conversion" {
			convertedReviewID = item.ReviewID
			break
		}
	}
	if convertedReviewID == "" {
		t.Fatalf("review list items = %+v, want converted intake goal review item", listed.Items)
	}

	show := run("review", "show", "--id", convertedReviewID, "--json")
	if !strings.Contains(show, `"review_id": "`+convertedReviewID+`"`) || !strings.Contains(show, `"source_type": "intake_goal_conversion"`) || !strings.Contains(show, `"goal_id": 1`) {
		t.Fatalf("review show output = %s, want one converted intake goal item", show)
	}
}

func TestRunReviewApproveGoalDerivedItems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}
	createGoal := func(title string) int64 {
		t.Helper()
		output := run("goal", "create", "--title", title, "--json")
		created := decodeGoalEnvelope(t, []byte(output))
		return created.ID
	}
	assertGoalStatus := func(goalID int64, want string) {
		t.Helper()
		output := run("goal", "show", "--id", int64String(goalID), "--json")
		shown := decodeGoalEnvelope(t, []byte(output))
		if shown.Status != want {
			t.Fatalf("goal %d status = %q, want %q\noutput=%s", goalID, shown.Status, want, output)
		}
	}

	run("intake", "raw", "create", "--text", "Build a browser executor for Odin research goals", "--json")
	run("intake", "process", "--id", "intake-1", "--json")
	intakeApproved := run("review", "approve", "--id", "intake-goal:1", "--json")
	if !strings.Contains(intakeApproved, `"review_id": "intake-goal:1"`) || !strings.Contains(intakeApproved, `"status": "approved_for_execution"`) {
		t.Fatalf("review approve intake-goal output = %s, want approved goal", intakeApproved)
	}
	assertGoalStatus(1, string(sqlite.GoalStatusApprovedForExecution))

	tick := run("goal", "tick", "--json")
	if !strings.Contains(tick, `"started": 1`) || !strings.Contains(tick, `"goal_id": 1`) {
		t.Fatalf("goal tick output = %s, want approved goal picked up", tick)
	}

	createdGoalID := createGoal("Approve created review goal")
	createdApproved := run("review", "approve", "--id", "goal:"+int64String(createdGoalID), "--json")
	if !strings.Contains(createdApproved, `"review_id": "goal:`+int64String(createdGoalID)+`"`) || !strings.Contains(createdApproved, `"status": "approved_for_execution"`) {
		t.Fatalf("review approve goal output = %s, want approved goal", createdApproved)
	}
	assertGoalStatus(createdGoalID, string(sqlite.GoalStatusApprovedForExecution))

	plannedGoalID := createGoal("Approve planned review goal")
	run("goal", "transition", "--id", int64String(plannedGoalID), "--status", "planned", "--json")
	plannedApproved := run("review", "approve", "--id", "goal-approval:"+int64String(plannedGoalID), "--json")
	if !strings.Contains(plannedApproved, `"review_id": "goal-approval:`+int64String(plannedGoalID)+`"`) || !strings.Contains(plannedApproved, `"status": "approved_for_execution"`) {
		t.Fatalf("review approve goal-approval output = %s, want approved goal", plannedApproved)
	}
	assertGoalStatus(plannedGoalID, string(sqlite.GoalStatusApprovedForExecution))

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	blockedGoal, err := store.CreateGoal(context.Background(), sqlite.CreateGoalParams{Title: "Blocked review approval unsupported"})
	if err != nil {
		t.Fatalf("CreateGoal(blocked) error = %v", err)
	}
	if _, err := store.TransitionGoal(context.Background(), sqlite.TransitionGoalParams{GoalID: blockedGoal.ID, Status: sqlite.GoalStatusBlocked}); err != nil {
		t.Fatalf("TransitionGoal(blocked) error = %v", err)
	}
	blocker, err := store.AddGoalBlocker(context.Background(), sqlite.AddGoalBlockerParams{
		GoalID:      blockedGoal.ID,
		Status:      "open",
		BlockerType: "operator_action",
		Summary:     "operator must resolve blocker",
		CreatedBy:   "test",
	})
	if err != nil {
		t.Fatalf("AddGoalBlocker() error = %v", err)
	}
	store.Close()

	var unsupportedOut bytes.Buffer
	err = Run(context.Background(), root, []string{"review", "approve", "--id", "goal-blocker:" + int64String(blocker.ID), "--json"}, strings.NewReader(""), &unsupportedOut)
	if err == nil || !strings.Contains(err.Error(), "review approve does not support goal-blocker") {
		t.Fatalf("Run(review approve goal-blocker) error = %v output=%s, want unsupported-action error", err, unsupportedOut.String())
	}

	logs := run("logs", "--json")
	if strings.Count(logs, `"type": "review.approved"`) != 3 || !strings.Contains(logs, `"type": "goal.status_changed"`) {
		t.Fatalf("logs output = %s, want review.approved and goal.status_changed audit events", logs)
	}
}

func TestRunReviewRejectGoalDerivedItems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}
	createGoal := func(title string) int64 {
		t.Helper()
		output := run("goal", "create", "--title", title, "--json")
		created := decodeGoalEnvelope(t, []byte(output))
		return created.ID
	}
	assertGoalStatus := func(goalID int64, want string) {
		t.Helper()
		output := run("goal", "show", "--id", int64String(goalID), "--json")
		shown := decodeGoalEnvelope(t, []byte(output))
		if shown.Status != want {
			t.Fatalf("goal %d status = %q, want %q\noutput=%s", goalID, shown.Status, want, output)
		}
	}

	run("intake", "raw", "create", "--text", "Build a browser executor for Odin research goals", "--json")
	run("intake", "process", "--id", "intake-1", "--json")
	intakeRejected := run("review", "reject", "--id", "intake-goal:1", "--reason", "not ready", "--json")
	if !strings.Contains(intakeRejected, `"review_id": "intake-goal:1"`) || !strings.Contains(intakeRejected, `"decision": "rejected"`) || !strings.Contains(intakeRejected, `"status": "blocked"`) || !strings.Contains(intakeRejected, `"blocker"`) {
		t.Fatalf("review reject intake-goal output = %s, want blocked rejected goal with blocker", intakeRejected)
	}
	assertGoalStatus(1, string(sqlite.GoalStatusBlocked))

	tick := run("goal", "tick", "--json")
	if !strings.Contains(tick, `"started": 0`) {
		t.Fatalf("goal tick output = %s, want rejected blocked goal not executed", tick)
	}
	list := run("review", "list", "--json")
	if strings.Contains(list, `"review_id": "intake-goal:1"`) || !strings.Contains(list, `"source_type": "goal_blocker"`) {
		t.Fatalf("review list output = %s, want rejected item represented only as blocked goal", list)
	}

	createdGoalID := createGoal("Reject created review goal")
	createdRejected := run("review", "reject", "--id", "goal:"+int64String(createdGoalID), "--reason", "not ready", "--json")
	if !strings.Contains(createdRejected, `"review_id": "goal:`+int64String(createdGoalID)+`"`) || !strings.Contains(createdRejected, `"status": "blocked"`) {
		t.Fatalf("review reject goal output = %s, want blocked rejected goal", createdRejected)
	}
	assertGoalStatus(createdGoalID, string(sqlite.GoalStatusBlocked))

	plannedGoalID := createGoal("Reject planned review goal")
	run("goal", "transition", "--id", int64String(plannedGoalID), "--status", "planned", "--json")
	plannedRejected := run("review", "reject", "--id", "goal-approval:"+int64String(plannedGoalID), "--reason", "not ready", "--json")
	if !strings.Contains(plannedRejected, `"review_id": "goal-approval:`+int64String(plannedGoalID)+`"`) || !strings.Contains(plannedRejected, `"status": "blocked"`) {
		t.Fatalf("review reject goal-approval output = %s, want blocked rejected goal", plannedRejected)
	}
	assertGoalStatus(plannedGoalID, string(sqlite.GoalStatusBlocked))

	rawShow := run("intake", "raw", "show", "intake-1", "--json")
	if !strings.Contains(rawShow, `"goal_id": 1`) || !strings.Contains(rawShow, `"status": "review_required"`) {
		t.Fatalf("intake raw show output = %s, want preserved intake goal link", rawShow)
	}

	allBlockedTick := run("goal", "tick", "--json")
	if !strings.Contains(allBlockedTick, `"started": 0`) || strings.Count(allBlockedTick, `"action": "skipped"`) != 3 {
		t.Fatalf("goal tick output = %s, want all rejected goals skipped", allBlockedTick)
	}

	list = run("review", "list", "--json")
	for _, blockedReviewID := range []string{
		`"review_id": "intake-goal:1"`,
		`"review_id": "goal:` + int64String(createdGoalID) + `"`,
		`"review_id": "goal-approval:` + int64String(plannedGoalID) + `"`,
	} {
		if strings.Contains(list, blockedReviewID) {
			t.Fatalf("review list output = %s, want %s removed as normal pending review", list, blockedReviewID)
		}
	}
	if strings.Count(list, `"source_type": "goal_blocker"`) != 3 || !strings.Contains(list, `"review_id": "goal-blocker:1"`) || !strings.Contains(list, `"review_id": "goal-blocker:2"`) || !strings.Contains(list, `"review_id": "goal-blocker:3"`) {
		t.Fatalf("review list output = %s, want three blocker review items", list)
	}

	var unsupportedOut bytes.Buffer
	err := Run(context.Background(), root, []string{"review", "reject", "--id", "goal-blocker:1", "--reason", "still blocked", "--json"}, strings.NewReader(""), &unsupportedOut)
	if err == nil || !strings.Contains(err.Error(), "review reject does not support goal-blocker") {
		t.Fatalf("Run(review reject goal-blocker) error = %v output=%s, want unsupported-action error", err, unsupportedOut.String())
	}

	logs := run("logs", "--json")
	if strings.Count(logs, `"type": "review.rejected"`) != 3 || !strings.Contains(logs, `"type": "goal.blocker_recorded"`) || !strings.Contains(logs, `"type": "goal.status_changed"`) {
		t.Fatalf("logs output = %s, want review.rejected, blocker, and status audit events", logs)
	}
}

func TestRunReviewGoalBlockerActionsAreExplicitUnsupportedJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	run("intake", "raw", "create", "--text", "Build a browser executor for Odin research goals", "--json")
	run("intake", "process", "--id", "intake-1", "--json")
	rejected := run("review", "reject", "--id", "intake-goal:1", "--reason", "not ready", "--json")
	if !strings.Contains(rejected, `"blocker"`) {
		t.Fatalf("review reject output = %s, want blocker", rejected)
	}

	list := run("review", "list", "--json")
	if !strings.Contains(list, `"review_id": "goal-blocker:1"`) || !strings.Contains(list, `"source_type": "goal_blocker"`) || !strings.Contains(list, `"allowed_actions": []`) {
		t.Fatalf("review list output = %s, want visible unsupported goal blocker item", list)
	}

	show := run("review", "show", "--id", "goal-blocker:1", "--json")
	if !strings.Contains(show, `"review_id": "goal-blocker:1"`) || !strings.Contains(show, `"blocker_type": "review_rejected"`) || !strings.Contains(show, `"status": "blocked"`) {
		t.Fatalf("review show output = %s, want blocked goal blocker detail", show)
	}

	var approveOut bytes.Buffer
	err := Run(context.Background(), root, []string{"review", "approve", "--id", "goal-blocker:1", "--json"}, strings.NewReader(""), &approveOut)
	if err == nil || !strings.Contains(err.Error(), "review approve does not support goal-blocker") {
		t.Fatalf("Run(review approve goal-blocker) error = %v output=%s, want unsupported-action error", err, approveOut.String())
	}
	if !strings.Contains(approveOut.String(), `"status": "unsupported"`) || !strings.Contains(approveOut.String(), `"result": "not_resolved"`) || !strings.Contains(approveOut.String(), `"action": "approve"`) || !strings.Contains(approveOut.String(), `"review_id": "goal-blocker:1"`) {
		t.Fatalf("review approve unsupported output = %s, want machine-readable unsupported JSON", approveOut.String())
	}

	var rejectOut bytes.Buffer
	err = Run(context.Background(), root, []string{"review", "reject", "--id", "goal-blocker:1", "--reason", "still blocked", "--json"}, strings.NewReader(""), &rejectOut)
	if err == nil || !strings.Contains(err.Error(), "review reject does not support goal-blocker") {
		t.Fatalf("Run(review reject goal-blocker) error = %v output=%s, want unsupported-action error", err, rejectOut.String())
	}
	if !strings.Contains(rejectOut.String(), `"status": "unsupported"`) || !strings.Contains(rejectOut.String(), `"result": "not_resolved"`) || !strings.Contains(rejectOut.String(), `"action": "reject"`) || !strings.Contains(rejectOut.String(), `"review_id": "goal-blocker:1"`) {
		t.Fatalf("review reject unsupported output = %s, want machine-readable unsupported JSON", rejectOut.String())
	}

	goal := decodeGoalEnvelope(t, []byte(run("goal", "show", "--id", "1", "--json")))
	if goal.Status != string(sqlite.GoalStatusBlocked) {
		t.Fatalf("goal status = %q, want blocked after unsupported blocker actions", goal.Status)
	}

	logs := run("logs", "--json")
	if strings.Contains(logs, `"type": "review.approved"`) || strings.Count(logs, `"type": "review.rejected"`) != 1 {
		t.Fatalf("logs output = %s, want unsupported blocker actions without approval/rejection audit mutation", logs)
	}
}

func TestRunUnifiedReviewQueueSurfacesFailedWorkRetryPolicy(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")
	t.Setenv("ODIN_CODEX_DRIVER_RUN_RESPONSE", `{"status":"failed","output":"failed work review proof"}`)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	installRepoCodexDriverScript(t, root)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"failed work review proof"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}
	failTask := func(taskKey string, attempt int) {
		t.Helper()
		dispatched := run("work", "dispatch", "--task", taskKey, "--json")
		if !strings.Contains(dispatched, fmt.Sprintf(`"attempt": %d`, attempt)) || !strings.Contains(dispatched, `"status": "running"`) {
			t.Fatalf("dispatch output = %s, want attempt %d running", dispatched, attempt)
		}
		executed := run("work", "execute", "--task", taskKey, "--json")
		if !strings.Contains(executed, `"status": "failed"`) {
			t.Fatalf("execute output = %s, want terminal failure", executed)
		}
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "failed work review proof")
	run(
		"intake", "raw", "create",
		"--source", "operator",
		"--project", testProjectKey,
		"--title", "failed work review proof",
		"--type", "request",
		"--dedup-key", "failed-work-review-proof",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	)
	run("intake", "process", "--id", "intake-1", "--json")
	run("review", "act", "intake-review:1", "accept", "--json")
	failTask("intake-review-1", 1)

	list := run("review", "list", "--json")
	for _, want := range []string{
		`"queue_id": "failed-work:1"`,
		`"source_type": "failed_work"`,
		`"object_key": "intake-review-1"`,
		`"status": "failed"`,
		`"retry_eligible": true`,
		`"allowed_actions": [`,
		`"retry"`,
	} {
		if !strings.Contains(list, want) {
			t.Fatalf("review list output = %s, want %s", list, want)
		}
	}

	show := run("review", "show", "failed-work:1", "--json")
	for _, want := range []string{
		`"source_type": "failed_work"`,
		`"task_key": "intake-review-1"`,
		`"retry_eligible": true`,
		`"decision": "retry_allowed"`,
		`"recovery_recommendation": "Retry is allowed; dispatch the queued task to create the next run attempt."`,
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("review show output = %s, want %s", show, want)
		}
	}

	retried := run("review", "act", "failed-work:1", "retry", "--json")
	if !strings.Contains(retried, `"retried": true`) || !strings.Contains(retried, `"decision": "retry_allowed"`) || !strings.Contains(retried, `"status": "queued"`) {
		t.Fatalf("review retry output = %s, want bounded retry success", retried)
	}
	repeatList := run("review", "list", "--json")
	if strings.Contains(repeatList, `"queue_id": "failed-work:1"`) {
		t.Fatalf("review list output = %s, want queued retried work removed from failed-work queue", repeatList)
	}

	failTask("intake-review-1", 2)
	retried = run("review", "act", "failed-work:1", "retry", "--json")
	if !strings.Contains(retried, `"retried": true`) || !strings.Contains(retried, `"retry_count": 2`) {
		t.Fatalf("second review retry output = %s, want second bounded retry", retried)
	}
	failTask("intake-review-1", 3)

	blockedList := run("review", "list", "--json")
	for _, want := range []string{
		`"queue_id": "failed-work:1"`,
		`"retry_eligible": false`,
		`"retry_block_reason": "retry_blocked_max_attempts"`,
		`"recovery_recommendation": "Open a follow-up or adjust the task before retrying; max attempts reached."`,
	} {
		if !strings.Contains(blockedList, want) {
			t.Fatalf("blocked review list output = %s, want %s", blockedList, want)
		}
	}
	blockedRetry := run("review", "act", "failed-work:1", "retry", "--json")
	if !strings.Contains(blockedRetry, `"retried": false`) || !strings.Contains(blockedRetry, `"decision": "retry_blocked_max_attempts"`) || !strings.Contains(blockedRetry, `"retry_eligible": false`) {
		t.Fatalf("blocked review retry output = %s, want policy block", blockedRetry)
	}
	runsOutput := run("runs", "--json")
	if strings.Count(runsOutput, `"task_key": "intake-review-1"`) != 3 {
		t.Fatalf("runs output = %s, want no fourth run after blocked review retry", runsOutput)
	}
	logsOutput := run("logs", "--json")
	if !strings.Contains(logsOutput, `"type": "task.retry_evaluated"`) || !strings.Contains(logsOutput, `"decision": "retry_blocked_max_attempts"`) {
		t.Fatalf("logs output = %s, want retry evaluation audit evidence", logsOutput)
	}
	statusOutput := run("work", "status")
	if !strings.Contains(statusOutput, "failed_retryable_work_items=0") || !strings.Contains(statusOutput, "retry_blocked_work_items=1") {
		t.Fatalf("work status output = %s, want blocked retry counts", statusOutput)
	}
}

func TestRunLogsIncludeProjectScopedIntakeEventsForOdinCoreScope(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"odin core scoped intake log proof"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	run("project", "select", "odin-core")
	run(
		"intake", "raw", "create",
		"--source", "operator",
		"--project", "odin-core",
		"--title", "Build Odin core scoped intake log proof",
		"--type", "request",
		"--dedup-key", "odin-core-intake-log-proof",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	)
	run("intake", "process", "--id", "intake-1", "--json")
	run("intake", "review", "accept", "intake-1", "--json")

	logsOutput := run("logs", "--json")
	for _, want := range []string{
		`"type": "intake.review_accepted"`,
		`"scope": "project"`,
		`"project_id":`,
	} {
		if !strings.Contains(logsOutput, want) {
			t.Fatalf("logs output = %s, want %s", logsOutput, want)
		}
	}
}

func TestRunIntakeLifecycleIsVisibleInProjectLogsAndOverview(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"operator auditability proof"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}
	createRaw := func(title, dedup string) {
		t.Helper()
		run(
			"intake", "raw", "create",
			"--source", "operator",
			"--project", testProjectKey,
			"--title", title,
			"--type", "request",
			"--dedup-key", dedup,
			"--requested-by", "codex",
			"--payload-file", payloadPath,
			"--json",
		)
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "intake auditability test")

	createRaw("Prepare weekly status summary", "audit-clear")
	createRaw("Help with this", "audit-vague")
	createRaw("Prepare weekly status summary duplicate", "audit-clear")
	createRaw("Delete production cache after approval", "audit-risk-approve")
	createRaw("Delete production archive after approval", "audit-risk-deny")

	for _, id := range []string{"intake-1", "intake-2", "intake-3", "intake-4", "intake-5"} {
		run("intake", "process", "--id", id, "--json")
	}
	run("intake", "review", "accept", "intake-1", "--json")
	run("intake", "review", "accept", "intake-4", "--json")
	run("intake", "review", "accept", "intake-5", "--json")
	run("intake", "approval", "approve", "intake-4", "--json")
	run("intake", "approval", "deny", "intake-5", "--json")

	logsOutput := run("logs", "--json")
	for _, want := range []string{
		`"stream_type": "intake_item"`,
		`"stream_id": 1`,
		`"project_id":`,
		`"type": "intake.processing_started"`,
		`"type": "intake.classified"`,
		`"type": "intake.dedupe_reviewed"`,
		`"type": "intake.routed"`,
		`"type": "intake.draft_artifact_created"`,
		`"type": "intake.clarification_needed"`,
		`"type": "intake.duplicate_linked_or_suppressed"`,
		`"type": "intake.review_accepted"`,
		`"type": "intake.review_approval_required"`,
		`"type": "intake.approval_approved"`,
		`"type": "intake.approval_denied"`,
		`"intake_item_id": 4`,
		`"policy_reason": "operator_approved_risky_intake"`,
		`"policy_reason": "operator_denied_risky_intake"`,
	} {
		if !strings.Contains(logsOutput, want) {
			t.Fatalf("logs output = %s, want %s", logsOutput, want)
		}
	}

	overviewOutput := run("overview", "--json")
	for _, want := range []string{
		`"intake_inbox":`,
		`"wiring": "live"`,
		`"raw_item_count": 5`,
		`"raw_processed_count": 5`,
		`"review_queue_count": 2`,
		`"accepted_count": 2`,
		`"needs_clarification_count": 1`,
		`"duplicate_linked_or_suppressed_count": 1`,
		`"approval_denied_count": 1`,
		`"key": "intake-4"`,
		`"status": "accepted"`,
		`"key": "intake-5"`,
		`"status": "approval_denied"`,
	} {
		if !strings.Contains(overviewOutput, want) {
			t.Fatalf("overview output = %s, want %s", overviewOutput, want)
		}
	}
}

func TestRunHelpIncludesOverviewCommand(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	if err := Run(context.Background(), root, []string{"help"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(help) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "overview") {
		t.Fatalf("help output = %q, want overview command", stdout.String())
	}
	if !strings.Contains(stdout.String(), "runs show <id>") {
		t.Fatalf("help output = %q, want top-level runs show command", stdout.String())
	}
	if !strings.Contains(stdout.String(), "scheduler") {
		t.Fatalf("help output = %q, want scheduler command", stdout.String())
	}
}

func TestRunSchedulerTickUsesExistingRuntimePaths(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)

	if err := Run(context.Background(), root, []string{
		"trigger", "upsert", "scheduler-proof",
		"initiative=odin-core",
		"kind=schedule",
		"status=enabled",
		"next=2026-05-02T00:00:00Z",
		"title=Scheduler_proof",
		"--json",
	}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(trigger upsert) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{
		"scheduler", "tick",
		"now=2026-05-02T00:00:00Z",
		"recovery=false",
		"--json",
	}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(scheduler tick) error = %v", err)
	}
	for _, want := range []string{
		`"now": "2026-05-02T00:00:00Z"`,
		`"trigger_evaluation"`,
		`"evaluated": 1`,
		`"materialized": 1`,
		`"supervision"`,
		`"recovery_ran": false`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("scheduler tick output = %s, want %s", stdout.String(), want)
		}
	}

	var logs bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logs); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "automation_trigger.fire_requested"`,
		`"type": "automation_trigger.materialized"`,
		`"key": "scheduler-proof"`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs output = %s, want %s", logs.String(), want)
		}
	}
}

func TestRunWorkStartAndStatusUseCanonicalCommandPath(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)

	var startOutput bytes.Buffer
	err := Run(context.Background(), root, []string{"work", "start", "--project", "odin-core", "--title", "Implement delivery surface"}, strings.NewReader(""), &startOutput)
	if err != nil {
		t.Fatalf("Run(work start) error = %v", err)
	}

	for _, want := range []string{
		"work_item_id=",
		"project=odin-core",
		"status=queued",
	} {
		if !strings.Contains(startOutput.String(), want) {
			t.Fatalf("Run(work start) output = %q, want %q", startOutput.String(), want)
		}
	}

	var statusOutput bytes.Buffer
	err = Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &statusOutput)
	if err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	for _, want := range []string{
		"work_items=1",
		"open_work_items=1",
	} {
		if !strings.Contains(statusOutput.String(), want) {
			t.Fatalf("Run(work status) output = %q, want %q", statusOutput.String(), want)
		}
	}
}

func TestRunWorkDispatchCreatesRunAttemptFromAcceptedIntake(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"prepare weekly summary"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "delegation inspection test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "delegation operator proof"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "dispatch test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	if err := Run(context.Background(), root, []string{
		"intake", "raw", "create",
		"--source", "operator",
		"--project", "alpha-cli",
		"--title", "Prepare weekly summary",
		"--type", "request",
		"--dedup-key", "dispatch-intake",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake raw create) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake process) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"intake", "review", "accept", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake review accept) error = %v", err)
	}

	var dispatchOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "dispatch", "--task", "intake-review-1", "--json"}, strings.NewReader(""), &dispatchOutput); err != nil {
		t.Fatalf("Run(work dispatch) error = %v", err)
	}
	var dispatch struct {
		Dispatched bool   `json:"dispatched"`
		Reason     string `json:"reason"`
		Task       struct {
			ID     int64  `json:"id"`
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"task"`
		Run *struct {
			ID       int64  `json:"id"`
			TaskID   int64  `json:"task_id"`
			Executor string `json:"executor"`
			Status   string `json:"status"`
			Attempt  int    `json:"attempt"`
		} `json:"run"`
	}
	if err := json.Unmarshal(dispatchOutput.Bytes(), &dispatch); err != nil {
		t.Fatalf("json.Unmarshal(dispatch) error = %v\n%s", err, dispatchOutput.String())
	}
	if !dispatch.Dispatched || dispatch.Reason != "dispatched" || dispatch.Task.Key != "intake-review-1" || dispatch.Task.Status != "running" {
		t.Fatalf("dispatch output = %+v, want running dispatched intake work", dispatch)
	}
	if dispatch.Run == nil || dispatch.Run.ID == 0 || dispatch.Run.TaskID != dispatch.Task.ID || dispatch.Run.Executor != "codex_headless" || dispatch.Run.Status != "running" || dispatch.Run.Attempt != 1 {
		t.Fatalf("dispatch run = %+v, want correlated running codex_headless attempt", dispatch.Run)
	}

	var repeatOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "dispatch", "--task", "intake-review-1", "--json"}, strings.NewReader(""), &repeatOutput); err != nil {
		t.Fatalf("Run(work dispatch repeat) error = %v", err)
	}
	var repeat struct {
		Dispatched bool   `json:"dispatched"`
		Reason     string `json:"reason"`
		Task       struct {
			ID     int64  `json:"id"`
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"task"`
		Run *struct {
			ID     int64  `json:"id"`
			TaskID int64  `json:"task_id"`
			Status string `json:"status"`
		} `json:"run"`
	}
	if err := json.Unmarshal(repeatOutput.Bytes(), &repeat); err != nil {
		t.Fatalf("json.Unmarshal(repeat dispatch) error = %v\n%s", err, repeatOutput.String())
	}
	if repeat.Dispatched || repeat.Reason != "task_not_queued" || repeat.Run == nil || repeat.Run.ID != dispatch.Run.ID || repeat.Run.TaskID != dispatch.Task.ID {
		t.Fatalf("repeat dispatch = %+v, want blocked with existing run", repeat)
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	for _, want := range []string{
		`"task_id": 1`,
		`"task_key": "intake-review-1"`,
		`"current_run_id": 1`,
		`"status": "running"`,
	} {
		if !strings.Contains(jobsOutput.String(), want) {
			t.Fatalf("jobs output = %s, want %s", jobsOutput.String(), want)
		}
	}

	var runsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"runs", "--json"}, strings.NewReader(""), &runsOutput); err != nil {
		t.Fatalf("Run(runs --json) error = %v", err)
	}
	for _, want := range []string{
		`"run_id": 1`,
		`"task_id": 1`,
		`"task_key": "intake-review-1"`,
		`"status": "running"`,
	} {
		if !strings.Contains(runsOutput.String(), want) {
			t.Fatalf("runs output = %s, want %s", runsOutput.String(), want)
		}
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "task.dispatch_requested"`,
		`"type": "run.started"`,
		`"type": "task.status_changed"`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}

	var statusOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &statusOutput); err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	for _, want := range []string{"work_items=1", "active_run_attempts=1", "dispatch=work_dispatch"} {
		if !strings.Contains(statusOutput.String(), want) {
			t.Fatalf("work status output = %s, want %s", statusOutput.String(), want)
		}
	}
}

func TestRunWorkDispatchFailsClosedForEmptyTaskArgument(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "empty dispatch task proof")
	run("work", "start", "--project", testProjectKey, "--title", "Neutral status proof", "--intent", "read_only")

	var dispatchOutput bytes.Buffer
	err := Run(context.Background(), root, []string{"work", "dispatch", "--task", "", "--json"}, strings.NewReader(""), &dispatchOutput)
	if err == nil {
		t.Fatalf("Run(work dispatch --task empty) error = nil output=%s, want fail-closed usage error", dispatchOutput.String())
	}
	if !strings.Contains(err.Error(), "usage: odin work dispatch --task <id|key> [--json]") {
		t.Fatalf("Run(work dispatch --task empty) error = %v, want usage error", err)
	}

	jobsOutput := run("jobs", "--json")
	if !strings.Contains(jobsOutput, `"status": "queued"`) || strings.Contains(jobsOutput, `"status": "running"`) {
		t.Fatalf("jobs output = %s, want queued task untouched by empty dispatch", jobsOutput)
	}
	runsOutput := run("runs", "--json")
	if !strings.Contains(runsOutput, `"runs": []`) {
		t.Fatalf("runs output = %s, want no run from empty dispatch", runsOutput)
	}
}

func TestRunWorkDispatchEnforcesProjectExecutionPolicy(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}
	parseTaskKey := func(output string) string {
		t.Helper()
		for _, field := range strings.Fields(output) {
			if value, ok := strings.CutPrefix(field, "key="); ok {
				return value
			}
		}
		t.Fatalf("work start output = %q, want task key", output)
		return ""
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "execution policy read-only proof")
	readOnlyKey := parseTaskKey(run("work", "start", "--project", testProjectKey, "--title", "Neutral status proof", "--intent", "read_only"))
	readOnlyDispatch := run("work", "dispatch", "--task", readOnlyKey, "--json")
	if !strings.Contains(readOnlyDispatch, `"dispatched": true`) || !strings.Contains(readOnlyDispatch, `"reason": "dispatched"`) || !strings.Contains(readOnlyDispatch, `"status": "running"`) || !strings.Contains(readOnlyDispatch, `"execution_intent": "read_only"`) || !strings.Contains(readOnlyDispatch, `"execution_intent_source": "operator"`) {
		t.Fatalf("read-only dispatch output = %s, want dispatched running task with explicit read-only intent", readOnlyDispatch)
	}

	mutationKey := parseTaskKey(run("work", "start", "--project", testProjectKey, "--title", "Neutral repo task", "--intent", "mutation"))
	mutationDispatch := run("work", "dispatch", "--task", mutationKey, "--json")
	if !strings.Contains(mutationDispatch, `"dispatched": false`) || !strings.Contains(mutationDispatch, `"reason": "mutation_requires_isolated_worktree"`) || !strings.Contains(mutationDispatch, `"status": "blocked"`) || !strings.Contains(mutationDispatch, `"execution_intent": "mutation"`) || !strings.Contains(mutationDispatch, `"execution_intent_source": "operator"`) {
		t.Fatalf("mutation dispatch output = %s, want direct mutation blocked by persisted operator intent", mutationDispatch)
	}
	runsAfterMutation := run("runs", "--json")
	if strings.Count(runsAfterMutation, `"task_key": "`) != 1 {
		t.Fatalf("runs output = %s, want only the read-only dispatch run", runsAfterMutation)
	}
	if !strings.Contains(runsAfterMutation, `"project_key": "alpha-cli"`) || !strings.Contains(runsAfterMutation, `"repo_root": "`) || !strings.Contains(runsAfterMutation, `"worktree_path": "`) || !strings.Contains(runsAfterMutation, `"branch_name": "main"`) {
		t.Fatalf("runs output = %s, want project/worktree/branch execution context", runsAfterMutation)
	}
	mutationLogs := run("logs", "--json")
	if !strings.Contains(mutationLogs, `"type": "task.queue_state_changed"`) || !strings.Contains(mutationLogs, `"blocked_reason": "mutation_requires_isolated_worktree"`) || !strings.Contains(mutationLogs, `"execution_intent": "mutation"`) || !strings.Contains(mutationLogs, `"execution_intent_source": "operator"`) {
		t.Fatalf("mutation logs output = %s, want mutation policy block evidence with persisted intent", mutationLogs)
	}

	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "execution policy approval proof")
	systemMutationKey := parseTaskKey(run("work", "start", "--project", "odin-core", "--title", "Neutral system task", "--intent", "governance"))
	systemMutationDispatch := run("work", "dispatch", "--task", systemMutationKey, "--json")
	if !strings.Contains(systemMutationDispatch, `"dispatched": false`) || !strings.Contains(systemMutationDispatch, `"reason": "approval_required"`) || !strings.Contains(systemMutationDispatch, `"status": "blocked"`) || !strings.Contains(systemMutationDispatch, `"execution_intent": "governance"`) {
		t.Fatalf("system mutation dispatch output = %s, want approval-required governance block from persisted intent", systemMutationDispatch)
	}

	logsOutput := run("logs", "--json")
	for _, want := range []string{
		`"type": "task.queue_state_changed"`,
		`"type": "approval.requested"`,
		`"blocked_reason": "approval_required"`,
		`"execution_intent_source": "operator"`,
	} {
		if !strings.Contains(logsOutput, want) {
			t.Fatalf("logs output = %s, want %s", logsOutput, want)
		}
	}
	statusOutput := run("work", "status")
	for _, want := range []string{
		"work_items=3",
		"open_work_items=3",
		"active_run_attempts=1",
		"pending_approvals=1",
		"explicit_intent_work_items=3",
	} {
		if !strings.Contains(statusOutput, want) {
			t.Fatalf("work status output = %s, want %s", statusOutput, want)
		}
	}
}

func TestRunWorkExplicitTriggerIntentDoesNotDependOnTitleInference(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}

	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "explicit trigger intent proof")
	run("trigger", "upsert", "neutral-governance-trigger",
		"initiative=odin-core",
		"kind=schedule",
		"status=enabled",
		"next=2026-05-05T00:00:00Z",
		"title=Neutral_periodic_check",
		"summary=neutral periodic check",
		"intent=governance",
		"--json",
	)
	fireOutput := run("trigger", "fire", "neutral-governance-trigger", "reason=explicit-intent-proof", "--json")
	var fire struct {
		WorkItem struct {
			Key                   string `json:"key"`
			ExecutionIntent       string `json:"execution_intent"`
			ExecutionIntentSource string `json:"execution_intent_source"`
		} `json:"work_item"`
	}
	if err := json.Unmarshal([]byte(fireOutput), &fire); err != nil {
		t.Fatalf("json.Unmarshal(fire) error = %v\n%s", err, fireOutput)
	}
	if fire.WorkItem.Key == "" || fire.WorkItem.ExecutionIntent != "governance" || fire.WorkItem.ExecutionIntentSource != "trigger" {
		t.Fatalf("trigger fire output = %+v, want trigger-persisted governance intent", fire)
	}

	dispatchOutput := run("work", "dispatch", "--task", fire.WorkItem.Key, "--json")
	if !strings.Contains(dispatchOutput, `"dispatched": false`) || !strings.Contains(dispatchOutput, `"reason": "approval_required"`) || !strings.Contains(dispatchOutput, `"execution_intent": "governance"`) || !strings.Contains(dispatchOutput, `"execution_intent_source": "trigger"`) {
		t.Fatalf("dispatch output = %s, want approval gated by trigger intent without risky title wording", dispatchOutput)
	}
	jobsOutput := run("jobs", "--json")
	if !strings.Contains(jobsOutput, `"execution_intent": "governance"`) || !strings.Contains(jobsOutput, `"execution_intent_source": "trigger"`) || !strings.Contains(jobsOutput, `"blocked_reason": "approval_required"`) {
		t.Fatalf("jobs output = %s, want persisted trigger intent and policy result", jobsOutput)
	}
	overviewOutput := run("overview", "--json")
	if !strings.Contains(overviewOutput, `"execution_intent": "governance"`) || !strings.Contains(overviewOutput, `"execution_intent_source": "trigger"`) {
		t.Fatalf("overview output = %s, want trigger-created work intent visibility", overviewOutput)
	}
}

func TestRunTriggerMVPUsesLiveOperatorLifecycle(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}

	helpOutput := run("trigger", "--help")
	for _, want := range []string{
		"Scheduled triggers:",
		"odin trigger upsert <key> initiative=<project> kind=schedule",
		"odin trigger evaluate now=<RFC3339>",
		"Manual trigger fire:",
		"odin trigger fire <key>",
	} {
		if !strings.Contains(helpOutput, want) {
			t.Fatalf("trigger help output = %s, want %s", helpOutput, want)
		}
	}

	run("project", "select", testProjectKey)
	upsertOutput := run("trigger", "upsert", "daily-review",
		"initiative="+testProjectKey,
		"kind=schedule",
		"status=enabled",
		"cadence=1h",
		"next=2026-05-05T00:00:00Z",
		"title=Run_daily_review",
		"summary=hourly_review",
		"--json",
	)
	if !strings.Contains(upsertOutput, `"key": "daily-review"`) || !strings.Contains(upsertOutput, `"status": "enabled"`) {
		t.Fatalf("trigger upsert output = %s, want enabled daily-review JSON", upsertOutput)
	}
	listOutput := run("trigger", "list", "--json")
	if !strings.Contains(listOutput, `"key": "daily-review"`) || !strings.Contains(listOutput, `"next_eligible_at": "2026-05-05T00:00:00Z"`) {
		t.Fatalf("trigger list output = %s, want daily-review with next eligible time", listOutput)
	}
	showOutput := run("trigger", "show", "daily-review", "--json")
	if !strings.Contains(showOutput, `"rule_summary": "hourly_review"`) || !strings.Contains(showOutput, `"work_item_title": "Run daily review"`) {
		t.Fatalf("trigger show output = %s, want reviewable trigger details", showOutput)
	}

	evaluateOutput := run("trigger", "evaluate", "now=2026-05-05T00:00:00Z", "--json")
	var evaluate struct {
		Evaluated    int `json:"evaluated"`
		Materialized int `json:"materialized"`
		Results      []struct {
			CreatedWorkItem bool `json:"created_work_item"`
			WorkItem        struct {
				ID     int64  `json:"id"`
				Key    string `json:"key"`
				Status string `json:"status"`
			} `json:"work_item"`
			Materialization struct {
				MaterializationKey string `json:"materialization_key"`
				Reason             string `json:"reason"`
				RequestedBy        string `json:"requested_by"`
			} `json:"materialization"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(evaluateOutput), &evaluate); err != nil {
		t.Fatalf("json.Unmarshal(evaluate) error = %v\n%s", err, evaluateOutput)
	}
	if evaluate.Evaluated != 1 || evaluate.Materialized != 1 || len(evaluate.Results) != 1 || !evaluate.Results[0].CreatedWorkItem {
		t.Fatalf("trigger evaluate output = %+v, want one materialized queued work item", evaluate)
	}
	if evaluate.Results[0].WorkItem.Status != "queued" || evaluate.Results[0].Materialization.Reason != "due-20260505t000000z" || evaluate.Results[0].Materialization.RequestedBy != "automation_trigger_evaluator" {
		t.Fatalf("trigger materialization = %+v, want queued scheduled provenance", evaluate.Results[0])
	}

	repeatEvaluateOutput := run("trigger", "evaluate", "now=2026-05-05T00:00:00Z", "--json")
	if !strings.Contains(repeatEvaluateOutput, `"evaluated": 0`) || !strings.Contains(repeatEvaluateOutput, `"materialized": 0`) {
		t.Fatalf("repeat trigger evaluate output = %s, want no duplicate due materialization", repeatEvaluateOutput)
	}

	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "trigger approval proof")
	run("trigger", "upsert", "risky-trigger",
		"initiative=odin-core",
		"kind=schedule",
		"status=enabled",
		"cadence=1h",
		"next=2026-05-05T00:00:00Z",
		"title=Review_system_trigger",
		"summary=system_trigger",
		"--json",
	)
	fireOutput := run("trigger", "fire", "risky-trigger", "reason=approval-proof", "--json")
	var fire struct {
		CreatedWorkItem bool `json:"created_work_item"`
		WorkItem        struct {
			ID     int64  `json:"id"`
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"work_item"`
		Materialization struct {
			MaterializationKey string `json:"materialization_key"`
		} `json:"materialization"`
	}
	if err := json.Unmarshal([]byte(fireOutput), &fire); err != nil {
		t.Fatalf("json.Unmarshal(fire) error = %v\n%s", err, fireOutput)
	}
	if !fire.CreatedWorkItem || fire.WorkItem.Status != "queued" || fire.Materialization.MaterializationKey == "" {
		t.Fatalf("trigger fire output = %+v, want queued risky work item with materialization key", fire)
	}
	repeatFireOutput := run("trigger", "fire", "risky-trigger", "reason=approval-proof", "--json")
	if !strings.Contains(repeatFireOutput, `"created_work_item": false`) || !strings.Contains(repeatFireOutput, fire.WorkItem.Key) {
		t.Fatalf("repeat trigger fire output = %s, want duplicate suppressed with existing work item", repeatFireOutput)
	}

	dispatchOutput := run("work", "dispatch", "--task", fire.WorkItem.Key, "--json")
	if !strings.Contains(dispatchOutput, `"dispatched": false`) || !strings.Contains(dispatchOutput, `"reason": "approval_required"`) || !strings.Contains(dispatchOutput, `"status": "blocked"`) {
		t.Fatalf("dispatch risky trigger work output = %s, want approval-required block", dispatchOutput)
	}
	approvalsOutput := run("approvals", "all", "--json")
	if !strings.Contains(approvalsOutput, `"status": "pending"`) || !strings.Contains(approvalsOutput, fmt.Sprintf(`"task_key": "%s"`, fire.WorkItem.Key)) {
		t.Fatalf("approvals output = %s, want pending task-backed trigger approval", approvalsOutput)
	}
	logsOutput := run("logs", "--json")
	for _, want := range []string{
		`"type": "automation_trigger.created"`,
		`"type": "automation_trigger.fire_requested"`,
		`"type": "automation_trigger.evaluated"`,
		`"type": "automation_trigger.materialized"`,
		`"created_work_item": false`,
		`"materialization_key": "default:risky-trigger:manual:approval-proof"`,
	} {
		if !strings.Contains(logsOutput, want) {
			t.Fatalf("logs output = %s, want %s", logsOutput, want)
		}
	}
	overviewOutput := run("overview", "--json")
	if !strings.Contains(overviewOutput, `"automation_triggers"`) || !strings.Contains(overviewOutput, `"trigger_count": 1`) || !strings.Contains(overviewOutput, `"last_work_item_key": "`+fire.WorkItem.Key+`"`) || !strings.Contains(overviewOutput, `"open_work_item_count": 2`) {
		t.Fatalf("overview output = %s, want trigger and queued/blocked work visibility", overviewOutput)
	}
}

func TestRunTriggerHumanizedTimingDefersQuietHoursAndCoalescesMissedRuns(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}

	run("project", "select", testProjectKey)
	quietBase := time.Now().UTC().AddDate(0, 0, 1)
	quietDueAt := time.Date(quietBase.Year(), quietBase.Month(), quietBase.Day(), 3, 0, 0, 0, time.UTC)
	quietEvaluateAt := time.Date(quietBase.Year(), quietBase.Month(), quietBase.Day(), 3, 15, 0, 0, time.UTC)
	quietDeferredUntil := time.Date(quietBase.Year(), quietBase.Month(), quietBase.Day(), 6, 0, 0, 0, time.UTC)
	quietDueText := quietDueAt.Format(time.RFC3339)
	quietEvaluateText := quietEvaluateAt.Format(time.RFC3339)
	quietDeferredText := quietDeferredUntil.Format(time.RFC3339)

	run("trigger", "upsert", "quiet-proof",
		"initiative="+testProjectKey,
		"kind=schedule",
		"status=enabled",
		"cadence=1h",
		"next="+quietDueText,
		"title=Quiet_hours_proof",
		"summary=quiet_hours_probe",
		"quiet=02:00-06:00",
		"--json",
	)
	quietEvaluate := run("trigger", "evaluate", "now="+quietEvaluateText, "--json")
	var quiet struct {
		Evaluated    int `json:"evaluated"`
		Materialized int `json:"materialized"`
		Deferred     int `json:"deferred"`
		Deferrals    []struct {
			Key           string `json:"key"`
			Reason        string `json:"reason"`
			DueAt         string `json:"due_at"`
			DeferredUntil string `json:"deferred_until"`
		} `json:"deferrals"`
	}
	if err := json.Unmarshal([]byte(quietEvaluate), &quiet); err != nil {
		t.Fatalf("json.Unmarshal(quiet evaluate) error = %v\n%s", err, quietEvaluate)
	}
	if quiet.Evaluated != 1 || quiet.Materialized != 0 || quiet.Deferred != 1 || len(quiet.Deferrals) != 1 {
		t.Fatalf("quiet evaluate = %+v, want one deferred and no materialized work", quiet)
	}
	if quiet.Deferrals[0].Key != "quiet-proof" || quiet.Deferrals[0].Reason != "quiet_hours" || quiet.Deferrals[0].DueAt != quietDueText || quiet.Deferrals[0].DeferredUntil != quietDeferredText {
		t.Fatalf("quiet deferral = %+v, want quiet-hours deferral to 06:00Z", quiet.Deferrals[0])
	}
	if jobsOutput := run("jobs", "--json"); !strings.Contains(jobsOutput, `"jobs": []`) {
		t.Fatalf("jobs output = %s, want no work during quiet-hours deferral", jobsOutput)
	}
	showDeferred := run("trigger", "show", "quiet-proof", "--json")
	if !strings.Contains(showDeferred, `"timing_status": "deferred"`) || !strings.Contains(showDeferred, `"next_eligible_at": "`+quietDeferredText+`"`) {
		t.Fatalf("show deferred output = %s, want deferred state visible", showDeferred)
	}

	release := run("trigger", "evaluate", "now="+quietDeferredText, "--json")
	if !strings.Contains(release, `"evaluated": 1`) || !strings.Contains(release, `"materialized": 1`) || !strings.Contains(release, `"created_work_item": true`) {
		t.Fatalf("release evaluate output = %s, want one materialized work item after quiet hours", release)
	}
	repeatRelease := run("trigger", "evaluate", "now="+quietDeferredText, "--json")
	if !strings.Contains(repeatRelease, `"evaluated": 0`) || !strings.Contains(repeatRelease, `"materialized": 0`) {
		t.Fatalf("repeat release output = %s, want no duplicate materialization", repeatRelease)
	}

	run("trigger", "upsert", "missed-proof",
		"initiative="+testProjectKey,
		"kind=schedule",
		"status=enabled",
		"cadence=1h",
		"next=2026-05-05T00:00:00Z",
		"title=Missed_run_proof",
		"summary=missed_probe",
		"--json",
	)
	missed := run("trigger", "evaluate", "now=2026-05-05T05:30:00Z", "--json")
	if !strings.Contains(missed, `"evaluated": 1`) || !strings.Contains(missed, `"materialized": 1`) || !strings.Contains(missed, `"materialization_key": "default:missed-proof:schedule:due-20260505t000000z"`) {
		t.Fatalf("missed evaluate output = %s, want one deterministic missed-window materialization", missed)
	}
	showMissed := run("trigger", "show", "missed-proof", "--json")
	if !strings.Contains(showMissed, `"next_eligible_at": "2026-05-05T06:00:00Z"`) {
		t.Fatalf("missed show output = %s, want next future cadence window after evaluation time", showMissed)
	}
	repeatMissed := run("trigger", "evaluate", "now=2026-05-05T05:30:00Z", "--json")
	if !strings.Contains(repeatMissed, `"evaluated": 0`) || !strings.Contains(repeatMissed, `"materialized": 0`) {
		t.Fatalf("repeat missed output = %s, want no duplicate missed materialization", repeatMissed)
	}
	run("trigger", "upsert", "missed-proof",
		"initiative="+testProjectKey,
		"kind=schedule",
		"status=disabled",
		"cadence=1h",
		"next=2026-05-05T06:00:00Z",
		"title=Missed_run_proof",
		"summary=missed_probe",
		"--json",
	)

	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "humanized trigger approval proof")
	run("trigger", "upsert", "risky-timing",
		"initiative=odin-core",
		"kind=schedule",
		"status=enabled",
		"cadence=1h",
		"next=2026-05-05T03:00:00Z",
		"title=Risky_timing_proof",
		"summary=risky_timing_probe",
		"quiet=02:00-06:00",
		"--json",
	)
	riskyQuiet := run("trigger", "evaluate", "now=2026-05-05T03:30:00Z", "--json")
	if !strings.Contains(riskyQuiet, `"deferred": 1`) || !strings.Contains(riskyQuiet, `"materialized": 0`) {
		t.Fatalf("risky quiet output = %s, want quiet-hours deferral before approval path", riskyQuiet)
	}
	riskyRelease := run("trigger", "evaluate", "now=2026-05-05T06:00:00Z", "--json")
	var risky struct {
		Results []struct {
			WorkItem struct {
				Key string `json:"key"`
			} `json:"work_item"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(riskyRelease), &risky); err != nil {
		t.Fatalf("json.Unmarshal(risky release) error = %v\n%s", err, riskyRelease)
	}
	if len(risky.Results) != 1 || risky.Results[0].WorkItem.Key == "" {
		t.Fatalf("risky release = %+v, want one trigger-created work item", risky)
	}
	dispatch := run("work", "dispatch", "--task", risky.Results[0].WorkItem.Key, "--json")
	if !strings.Contains(dispatch, `"reason": "approval_required"`) || !strings.Contains(dispatch, `"status": "blocked"`) {
		t.Fatalf("risky dispatch output = %s, want approval-required block after timing release", dispatch)
	}

	logsOutput := run("logs", "--json")
	for _, want := range []string{
		`"type": "automation_trigger.deferred"`,
		`"reason": "quiet_hours"`,
		`"deferred_until": "2026-05-05T06:00:00Z"`,
		`"type": "automation_trigger.materialized"`,
		`"type": "approval.requested"`,
	} {
		if !strings.Contains(logsOutput, want) {
			t.Fatalf("logs output = %s, want %s", logsOutput, want)
		}
	}
	overviewOutput := run("overview", "--json")
	if !strings.Contains(overviewOutput, `"key": "risky-timing"`) || !strings.Contains(overviewOutput, `"pending_approval_count": 1`) {
		t.Fatalf("overview output = %s, want risky timing trigger and pending approval", overviewOutput)
	}
}

func TestRunTriggerBatchingGroupsSchedulesAndPreservesApproval(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}

	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "batched trigger approval proof")
	for _, item := range []struct {
		key  string
		next string
	}{
		{key: "batch-cli-first", next: "2026-05-05T09:05:00Z"},
		{key: "batch-cli-second", next: "2026-05-05T09:20:00Z"},
	} {
		run("trigger", "upsert", item.key,
			"initiative=odin-core",
			"kind=schedule",
			"status=enabled",
			"next="+item.next,
			"title="+item.key,
			"batch=ops-review",
			"batch_window=1h",
			"intent=governance",
			"--json",
		)
	}

	tickOutput := run("scheduler", "tick", "now=2026-05-05T09:30:00Z", "recovery=false", "--json")
	var tick struct {
		TriggerEvaluation struct {
			Evaluated    int `json:"evaluated"`
			Materialized int `json:"materialized"`
		} `json:"trigger_evaluation"`
	}
	if err := json.Unmarshal([]byte(tickOutput), &tick); err != nil {
		t.Fatalf("json.Unmarshal(scheduler tick) error = %v\n%s", err, tickOutput)
	}
	if tick.TriggerEvaluation.Evaluated != 2 || tick.TriggerEvaluation.Materialized != 1 {
		t.Fatalf("scheduler tick = %+v, want two evaluated triggers and one batched work item", tick)
	}

	var list struct {
		Triggers []struct {
			Key             string `json:"key"`
			LastWorkItemKey string `json:"last_work_item_key"`
		} `json:"triggers"`
	}
	if err := json.Unmarshal([]byte(run("trigger", "list", "--json")), &list); err != nil {
		t.Fatalf("json.Unmarshal(trigger list) error = %v", err)
	}
	var sharedWorkItem string
	for _, trigger := range list.Triggers {
		if trigger.Key != "batch-cli-first" && trigger.Key != "batch-cli-second" {
			continue
		}
		if trigger.LastWorkItemKey == "" {
			t.Fatalf("trigger %s has no last work item in %+v", trigger.Key, list.Triggers)
		}
		if sharedWorkItem == "" {
			sharedWorkItem = trigger.LastWorkItemKey
			continue
		}
		if trigger.LastWorkItemKey != sharedWorkItem {
			t.Fatalf("batched trigger work item = %s, want shared %s", trigger.LastWorkItemKey, sharedWorkItem)
		}
	}
	if sharedWorkItem == "" {
		t.Fatalf("trigger list = %+v, want batched work item", list.Triggers)
	}

	dispatch := run("work", "dispatch", "--task", sharedWorkItem, "--json")
	if !strings.Contains(dispatch, `"reason": "approval_required"`) || !strings.Contains(dispatch, `"status": "blocked"`) || !strings.Contains(dispatch, `"execution_intent": "governance"`) {
		t.Fatalf("batched dispatch output = %s, want governance approval-required block", dispatch)
	}

	logs := run("logs", "--json")
	for _, want := range []string{
		`"key": "batch-cli-first"`,
		`"key": "batch-cli-second"`,
		`"requested_by": "automation_trigger_batch_evaluator"`,
		`"type": "approval.requested"`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want %s", logs, want)
		}
	}
}

func TestRunTriggerEventMVPUsesInternalEventsWithDedupeAndApprovalGates(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}
	parseWorkStart := func(output string) (int64, string) {
		t.Helper()
		var taskID int64
		var taskKey string
		for _, field := range strings.Fields(output) {
			if value, ok := strings.CutPrefix(field, "work_item_id="); ok {
				parsed, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					t.Fatalf("parse work_item_id from %q: %v", output, err)
				}
				taskID = parsed
			}
			if value, ok := strings.CutPrefix(field, "key="); ok {
				taskKey = value
			}
		}
		if taskID == 0 || taskKey == "" {
			t.Fatalf("work start output = %q, want id and key", output)
		}
		return taskID, taskKey
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "event trigger low risk proof")
	sourceID, sourceKey := parseWorkStart(run("work", "start", "--project", testProjectKey, "--title", "Event source low risk"))
	run("trigger", "upsert", "low-risk-event",
		"initiative="+testProjectKey,
		"kind=event",
		"status=enabled",
		"event=task.status_changed",
		"match_status=running",
		fmt.Sprintf("match_task_id=%d", sourceID),
		"title=Review_event_trigger_output",
		"summary=event_trigger_low_risk",
		"--json",
	)
	dispatchSource := run("work", "dispatch", "--task", sourceKey, "--json")
	if !strings.Contains(dispatchSource, `"dispatched": true`) || !strings.Contains(dispatchSource, `"status": "running"`) {
		t.Fatalf("source dispatch output = %s, want running source task", dispatchSource)
	}
	evaluateEvents := run("trigger", "evaluate", "source=events", "--json")
	var lowRisk struct {
		Evaluated    int `json:"evaluated"`
		Materialized int `json:"materialized"`
		Results      []struct {
			CreatedWorkItem bool `json:"created_work_item"`
			Materialization struct {
				MaterializationKey string `json:"materialization_key"`
				Reason             string `json:"reason"`
				RequestedBy        string `json:"requested_by"`
			} `json:"materialization"`
			WorkItem struct {
				Key    string `json:"key"`
				Status string `json:"status"`
			} `json:"work_item"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(evaluateEvents), &lowRisk); err != nil {
		t.Fatalf("json.Unmarshal(event evaluate) error = %v\n%s", err, evaluateEvents)
	}
	if lowRisk.Evaluated != 1 || lowRisk.Materialized != 1 || len(lowRisk.Results) != 1 || !lowRisk.Results[0].CreatedWorkItem {
		t.Fatalf("event evaluate output = %+v, want one event materialization", lowRisk)
	}
	if !strings.Contains(lowRisk.Results[0].Materialization.MaterializationKey, ":event:event-") || lowRisk.Results[0].Materialization.RequestedBy != "automation_trigger_event_evaluator" || lowRisk.Results[0].WorkItem.Status != "queued" {
		t.Fatalf("event materialization = %+v, want event provenance and queued work", lowRisk.Results[0])
	}
	repeatEvents := run("trigger", "evaluate", "source=events", "--json")
	if !strings.Contains(repeatEvents, `"evaluated": 1`) || !strings.Contains(repeatEvents, `"materialized": 0`) || !strings.Contains(repeatEvents, `"created_work_item": false`) || !strings.Contains(repeatEvents, lowRisk.Results[0].WorkItem.Key) {
		t.Fatalf("repeat event evaluate output = %s, want duplicate suppressed with existing work", repeatEvents)
	}

	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "event trigger approval proof")
	riskySourceID, riskySourceKey := parseWorkStart(run("work", "start", "--project", testProjectKey, "--title", "Event source risky"))
	run("trigger", "upsert", "risky-event",
		"initiative=odin-core",
		"kind=event",
		"status=enabled",
		"event=task.status_changed",
		"match_status=running",
		fmt.Sprintf("match_task_id=%d", riskySourceID),
		"title=Review_risky_event_trigger",
		"summary=event_trigger_risky",
		"--json",
	)
	run("work", "dispatch", "--task", riskySourceKey, "--json")
	riskyEvents := run("trigger", "evaluate", "source=events", "--json")
	var risky struct {
		Results []struct {
			CreatedWorkItem bool `json:"created_work_item"`
			WorkItem        struct {
				Key string `json:"key"`
			} `json:"work_item"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(riskyEvents), &risky); err != nil {
		t.Fatalf("json.Unmarshal(risky event evaluate) error = %v\n%s", err, riskyEvents)
	}
	var riskyWorkKey string
	for _, result := range risky.Results {
		if result.CreatedWorkItem && result.WorkItem.Key != "" {
			riskyWorkKey = result.WorkItem.Key
		}
	}
	if riskyWorkKey == "" {
		t.Fatalf("risky event evaluate = %+v, want risky trigger-created work", risky)
	}
	repeatRiskyEvents := run("trigger", "evaluate", "source=events", "--json")
	if !strings.Contains(repeatRiskyEvents, `"materialized": 0`) || !strings.Contains(repeatRiskyEvents, `"created_work_item": false`) || !strings.Contains(repeatRiskyEvents, riskyWorkKey) {
		t.Fatalf("repeat risky event evaluate output = %s, want duplicate suppressed with existing work", repeatRiskyEvents)
	}
	dispatchRisky := run("work", "dispatch", "--task", riskyWorkKey, "--json")
	if !strings.Contains(dispatchRisky, `"reason": "approval_required"`) || !strings.Contains(dispatchRisky, `"status": "blocked"`) {
		t.Fatalf("risky event dispatch output = %s, want approval required", dispatchRisky)
	}
	approvals := run("approvals", "all", "--json")
	if !strings.Contains(approvals, `"status": "pending"`) || !strings.Contains(approvals, riskyWorkKey) {
		t.Fatalf("approvals output = %s, want pending risky event approval", approvals)
	}
	logs := run("logs", "--json")
	for _, want := range []string{
		`"type": "automation_trigger.fire_requested"`,
		`"source": "event"`,
		`"source_event_type": "task.status_changed"`,
		`"type": "automation_trigger.materialized"`,
		`"created_work_item": false`,
		`"type": "approval.requested"`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want %s", logs, want)
		}
	}
	overview := run("overview", "--json")
	if !strings.Contains(overview, `"key": "risky-event"`) || !strings.Contains(overview, `"kind": "event"`) || !strings.Contains(overview, `"pending_approval_count": 1`) {
		t.Fatalf("overview output = %s, want event trigger and approval visibility", overview)
	}
}

func TestRunTriggerProducedFailedWorkSurfacesSelfHealingGuidance(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")
	t.Setenv("ODIN_CODEX_DRIVER_RUN_RESPONSE", `{"status":"failed","output":"trigger fixture failure proof"}`)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	installRepoCodexDriverScript(t, root)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}
	extractAutomationTaskKey := func(output string, triggerKey string) string {
		t.Helper()
		var payload struct {
			Results []struct {
				WorkItem struct {
					Key string `json:"key"`
				} `json:"work_item"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			t.Fatalf("json.Unmarshal(trigger evaluate) error = %v\n%s", err, output)
		}
		prefix := "automation-" + triggerKey + "-"
		for _, result := range payload.Results {
			if strings.HasPrefix(result.WorkItem.Key, prefix) {
				return result.WorkItem.Key
			}
		}
		t.Fatalf("trigger evaluate output = %s, want work item prefix %s", output, prefix)
		return ""
	}
	failTask := func(taskKey string, attempt int, proof string) {
		t.Helper()
		dispatch := run("work", "dispatch", "--task", taskKey, "--json")
		if !strings.Contains(dispatch, fmt.Sprintf(`"attempt": %d`, attempt)) || !strings.Contains(dispatch, `"status": "running"`) {
			t.Fatalf("dispatch output = %s, want attempt %d running", dispatch, attempt)
		}
		execute := run("work", "execute", "--task", taskKey, "--json")
		if !strings.Contains(execute, `"status": "failed"`) || !strings.Contains(execute, proof) {
			t.Fatalf("execute output = %s, want failed proof %q", execute, proof)
		}
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "trigger self healing proof")
	run("trigger", "upsert", "fail-schedule",
		"initiative="+testProjectKey,
		"kind=schedule",
		"status=enabled",
		"next=2026-05-04T00:00:00Z",
		"title=trigger schedule failure proof",
		"summary=trigger_failure_recovery",
		"--json",
	)
	scheduledTaskKey := extractAutomationTaskKey(run("trigger", "evaluate", "now=2026-05-04T01:00:00Z", "--json"), "fail-schedule")
	failTask(scheduledTaskKey, 1, "trigger fixture failure proof")

	show := run("review", "show", "failed-work:1", "--json")
	for _, want := range []string{
		`"source_type": "failed_work"`,
		`"task_key": "` + scheduledTaskKey + `"`,
		`"decision": "retry_allowed"`,
		`"recovery_recommendation": "Trigger-produced work failed. Inspect the trigger materialization and failed run logs, then retry only through odin review act failed-work ID retry or odin work retry within policy."`,
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("review show output = %s, want %s", show, want)
		}
	}
	overview := run("overview", "--json")
	for _, want := range []string{
		`"recovery_guidance"`,
		`"work_item_key": "` + scheduledTaskKey + `"`,
		`"work_kind": "automation_trigger"`,
		`"source": "automation_trigger"`,
		`"recovery_recommendation": "Trigger-produced work failed. Inspect the trigger materialization and failed run logs, then retry only through odin review act failed-work ID retry or odin work retry within policy."`,
	} {
		if !strings.Contains(overview, want) {
			t.Fatalf("overview output = %s, want %s", overview, want)
		}
	}
	logs := run("logs", "--json")
	for _, want := range []string{
		`"type": "task.recovery_recommended"`,
		`"source": "automation_trigger"`,
		`"retry_eligible": true`,
		`"recovery_recommendation": "Trigger-produced work failed. Inspect the trigger materialization and failed run logs, then retry only through odin review act failed-work ID retry or odin work retry within policy."`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want %s", logs, want)
		}
	}

	run("review", "act", "failed-work:1", "retry", "--json")
	failTask(scheduledTaskKey, 2, "trigger fixture failure proof")
	run("review", "act", "failed-work:1", "retry", "--json")
	failTask(scheduledTaskKey, 3, "trigger fixture failure proof")
	blockedShow := run("review", "show", "failed-work:1", "--json")
	for _, want := range []string{
		`"decision": "retry_blocked_max_attempts"`,
		`"retry_eligible": false`,
		`"recovery_recommendation": "Trigger-produced work reached the retry limit. Inspect the trigger rule and materialization, then open a follow-up or adjust task policy before any further retry."`,
	} {
		if !strings.Contains(blockedShow, want) {
			t.Fatalf("blocked review show output = %s, want %s", blockedShow, want)
		}
	}
	blockedRetry := run("review", "act", "failed-work:1", "retry", "--json")
	if !strings.Contains(blockedRetry, `"retried": false`) || !strings.Contains(blockedRetry, `"decision": "retry_blocked_max_attempts"`) {
		t.Fatalf("blocked retry output = %s, want bounded retry block", blockedRetry)
	}

	eventSource := run("work", "start", "--project", testProjectKey, "--title", "Event self healing source")
	var eventSourceID int64
	var eventSourceKey string
	for _, field := range strings.Fields(eventSource) {
		if value, ok := strings.CutPrefix(field, "work_item_id="); ok {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				t.Fatalf("parse work_item_id from %q: %v", eventSource, err)
			}
			eventSourceID = parsed
		}
		if value, ok := strings.CutPrefix(field, "key="); ok {
			eventSourceKey = value
		}
	}
	if eventSourceID == 0 || eventSourceKey == "" {
		t.Fatalf("work start output = %q, want event source id/key", eventSource)
	}
	run("trigger", "upsert", "fail-event",
		"initiative="+testProjectKey,
		"kind=event",
		"status=enabled",
		"event=task.status_changed",
		"match_status=running",
		fmt.Sprintf("match_task_id=%d", eventSourceID),
		"title=trigger event failure proof",
		"summary=trigger_event_failure_recovery",
		"--json",
	)
	run("work", "dispatch", "--task", eventSourceKey, "--json")
	eventTaskKey := extractAutomationTaskKey(run("trigger", "evaluate", "source=events", "--json"), "fail-event")
	failTask(eventTaskKey, 1, "trigger fixture failure proof")
	eventReviewList := run("review", "list", "--json")
	if !strings.Contains(eventReviewList, `"object_key": "`+eventTaskKey+`"`) || !strings.Contains(eventReviewList, `"source": "automation_trigger"`) {
		t.Fatalf("review list output = %s, want event-trigger failed work guidance", eventReviewList)
	}
	status := run("work", "status")
	if !strings.Contains(status, "failed_retryable_work_items=1") || !strings.Contains(status, "retry_blocked_work_items=1") {
		t.Fatalf("work status output = %s, want one retryable trigger failure and one blocked trigger failure", status)
	}
}

func TestRunTriggerGitHubIssueExternalEventAdapterMVP(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}
	extractCreatedTaskKey := func(output string, prefix string) string {
		t.Helper()
		var payload struct {
			Results []struct {
				CreatedWorkItem bool `json:"created_work_item"`
				WorkItem        struct {
					Key string `json:"key"`
				} `json:"work_item"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			t.Fatalf("json.Unmarshal(trigger evaluate) error = %v\n%s", err, output)
		}
		for _, result := range payload.Results {
			if result.CreatedWorkItem && strings.HasPrefix(result.WorkItem.Key, prefix) {
				return result.WorkItem.Key
			}
		}
		t.Fatalf("trigger evaluate output = %s, want created task prefix %s", output, prefix)
		return ""
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "github issue trigger proof")
	run("trigger", "upsert", "github-low",
		"initiative="+testProjectKey,
		"kind=event",
		"status=enabled",
		"event=external.github.issue",
		"match_status=opened",
		"match_provider=github",
		"match_repo=acme/alpha",
		"title=Review GitHub issue event",
		"summary=github_issue_external_event",
		"--json",
	)
	ingested := run("trigger", "ingest", "github-issue",
		"project="+testProjectKey,
		"repo=acme/alpha",
		"number=77",
		"action=opened",
		"title=Low risk GitHub issue",
		"body=prepare release checklist",
		"url=https://github.example/acme/alpha/issues/77",
		"--json",
	)
	for _, want := range []string{
		`"source": "github_issue"`,
		`"event_type": "external.github.issue"`,
		`"external_event_key": "github:issue:acme/alpha:77:opened"`,
		`"repo": "acme/alpha"`,
		`"number": 77`,
	} {
		if !strings.Contains(ingested, want) {
			t.Fatalf("ingest output = %s, want %s", ingested, want)
		}
	}
	lowRiskEvaluate := run("trigger", "evaluate", "source=events", "--json")
	lowRiskTaskKey := extractCreatedTaskKey(lowRiskEvaluate, "automation-github-low-")
	if !strings.Contains(lowRiskEvaluate, `"materialization_key": "default:github-low:event:external-github-issue-acme-alpha-77-opened"`) {
		t.Fatalf("event evaluate output = %s, want external stable materialization key", lowRiskEvaluate)
	}
	replayed := run("trigger", "ingest", "github-issue",
		"project="+testProjectKey,
		"repo=acme/alpha",
		"number=77",
		"action=opened",
		"title=Low risk GitHub issue",
		"body=prepare release checklist",
		"url=https://github.example/acme/alpha/issues/77",
		"--json",
	)
	if !strings.Contains(replayed, `"external_event_key": "github:issue:acme/alpha:77:opened"`) {
		t.Fatalf("replay ingest output = %s, want stable external event key", replayed)
	}
	repeatEvaluate := run("trigger", "evaluate", "source=events", "--json")
	if !strings.Contains(repeatEvaluate, `"materialized": 0`) || !strings.Contains(repeatEvaluate, `"created_work_item": false`) || !strings.Contains(repeatEvaluate, lowRiskTaskKey) {
		t.Fatalf("repeat evaluate output = %s, want duplicate external event suppressed", repeatEvaluate)
	}

	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "github issue approval proof")
	run("trigger", "upsert", "github-risky",
		"initiative=odin-core",
		"kind=event",
		"status=enabled",
		"event=external.github.issue",
		"match_status=opened",
		"match_provider=github",
		"match_repo=acme/odin-core",
		"title=Review risky GitHub issue event",
		"summary=github_issue_risky_event",
		"--json",
	)
	run("trigger", "ingest", "github-issue",
		"project=odin-core",
		"repo=acme/odin-core",
		"number=9",
		"action=opened",
		"title=Governance mutation request",
		"body=change system policy",
		"url=https://github.example/acme/odin-core/issues/9",
		"--json",
	)
	riskyEvaluate := run("trigger", "evaluate", "source=events", "--json")
	riskyTaskKey := extractCreatedTaskKey(riskyEvaluate, "automation-github-risky-")
	dispatch := run("work", "dispatch", "--task", riskyTaskKey, "--json")
	if !strings.Contains(dispatch, `"reason": "approval_required"`) || !strings.Contains(dispatch, `"status": "blocked"`) {
		t.Fatalf("risky dispatch output = %s, want approval gate", dispatch)
	}
	approvals := run("approvals", "all", "--json")
	if !strings.Contains(approvals, `"status": "pending"`) || !strings.Contains(approvals, riskyTaskKey) {
		t.Fatalf("approvals output = %s, want pending risky external approval", approvals)
	}
	logs := run("logs", "--json")
	for _, want := range []string{
		`"type": "external.github.issue"`,
		`"external_event_key": "github:issue:acme/odin-core:9:opened"`,
		`"type": "automation_trigger.materialized"`,
		`"source": "event"`,
		`"source_event_type": "external.github.issue"`,
		`"type": "approval.requested"`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want %s", logs, want)
		}
	}
	overview := run("overview", "--json")
	if !strings.Contains(overview, `"key": "github-risky"`) || !strings.Contains(overview, `"kind": "event"`) || !strings.Contains(overview, `"pending_approval_count": 1`) {
		t.Fatalf("overview output = %s, want risky GitHub trigger visibility", overview)
	}
}

func TestRunKnowledgeSearchAndContextPackAreReadOnly(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}
	extractCreatedTaskKey := func(output string) string {
		t.Helper()
		var payload struct {
			Results []struct {
				CreatedWorkItem bool `json:"created_work_item"`
				WorkItem        struct {
					Key string `json:"key"`
				} `json:"work_item"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			t.Fatalf("json.Unmarshal(trigger evaluate) error = %v\n%s", err, output)
		}
		for _, result := range payload.Results {
			if result.CreatedWorkItem {
				return result.WorkItem.Key
			}
		}
		t.Fatalf("trigger evaluate output = %s, want created task", output)
		return ""
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "knowledge context proof")
	run("trigger", "upsert", "knowledge-low",
		"initiative="+testProjectKey,
		"kind=event",
		"status=enabled",
		"event=external.github.issue",
		"match_status=opened",
		"match_provider=github",
		"match_repo=acme/alpha",
		"title=Review knowledge retrieval issue",
		"summary=knowledge_context_pack_event",
		"--json",
	)
	run("trigger", "ingest", "github-issue",
		"project="+testProjectKey,
		"repo=acme/alpha",
		"number=88",
		"action=opened",
		"title=Knowledge retrieval issue",
		"body=prepare context pack evidence",
		"url=https://github.example/acme/alpha/issues/88",
		"--json",
	)
	taskKey := extractCreatedTaskKey(run("trigger", "evaluate", "source=events", "--json"))
	beforeLogs := run("logs", "--json")

	search := run("knowledge", "search", "query=knowledge", "project="+testProjectKey, "--json")
	for _, want := range []string{
		`"read_only": true`,
		`"persistence": "none"`,
		`"project_key": "` + testProjectKey + `"`,
		`"kind": "task"`,
		`"kind": "event"`,
		`knowledge`,
	} {
		if !strings.Contains(search, want) {
			t.Fatalf("knowledge search output = %s, want %s", search, want)
		}
	}
	contextPack := run("knowledge", "context-pack", "task="+taskKey, "project="+testProjectKey, "--json")
	for _, want := range []string{
		`"read_only": true`,
		`"persistence": "none"`,
		`"object_type": "task"`,
		`"object_key": "` + taskKey + `"`,
		`"events"`,
		`"context_items"`,
		`external-github-issue-acme-alpha-88-opened`,
	} {
		if !strings.Contains(contextPack, want) {
			t.Fatalf("knowledge context-pack output = %s, want %s", contextPack, want)
		}
	}
	afterLogs := run("logs", "--json")
	if beforeLogs != afterLogs {
		t.Fatalf("knowledge commands mutated logs\nbefore=%s\nafter=%s", beforeLogs, afterLogs)
	}
	jobs := run("jobs", "--json")
	if strings.Count(jobs, `"task_key"`) != 1 || !strings.Contains(jobs, taskKey) {
		t.Fatalf("jobs output = %s, want only existing trigger-created work", jobs)
	}
	runs := run("runs", "--json")
	if !strings.Contains(runs, `"runs": []`) {
		t.Fatalf("runs output = %s, want no run creation from knowledge commands", runs)
	}
}

func TestRunKnowledgeContextPackProposalReviewLifecycle(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
			t.Fatalf("Run(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}
	extractCreatedTaskKey := func(output string) string {
		t.Helper()
		var payload struct {
			Results []struct {
				CreatedWorkItem bool `json:"created_work_item"`
				WorkItem        struct {
					Key string `json:"key"`
				} `json:"work_item"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			t.Fatalf("json.Unmarshal(trigger evaluate) error = %v\n%s", err, output)
		}
		for _, result := range payload.Results {
			if result.CreatedWorkItem {
				return result.WorkItem.Key
			}
		}
		t.Fatalf("trigger evaluate output = %s, want created task", output)
		return ""
	}
	extractPacketID := func(output string) int64 {
		t.Helper()
		var payload struct {
			Proposal struct {
				ID int64 `json:"id"`
			} `json:"proposal"`
		}
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			t.Fatalf("json.Unmarshal(proposal) error = %v\n%s", err, output)
		}
		if payload.Proposal.ID == 0 {
			t.Fatalf("proposal output = %s, want proposal id", output)
		}
		return payload.Proposal.ID
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "knowledge proposal proof")
	run("trigger", "upsert", "knowledge-proposal",
		"initiative="+testProjectKey,
		"kind=event",
		"status=enabled",
		"event=external.github.issue",
		"match_status=opened",
		"match_provider=github",
		"match_repo=acme/alpha",
		"title=Review proposal context issue",
		"summary=knowledge_context_pack_proposal_event",
		"--json",
	)
	run("trigger", "ingest", "github-issue",
		"project="+testProjectKey,
		"repo=acme/alpha",
		"number=89",
		"action=opened",
		"title=Knowledge proposal issue",
		"body=prepare proposed context pack evidence",
		"url=https://github.example/acme/alpha/issues/89",
		"--json",
	)
	taskKey := extractCreatedTaskKey(run("trigger", "evaluate", "source=events", "--json"))
	beforeLogs := run("logs", "--json")
	readOnly := run("knowledge", "context-pack", "task="+taskKey, "project="+testProjectKey, "--json")
	afterReadOnlyLogs := run("logs", "--json")
	if beforeLogs != afterReadOnlyLogs || !strings.Contains(readOnly, `"persistence": "none"`) {
		t.Fatalf("read-only context pack mutated logs or omitted persistence marker\nbefore=%s\nafter=%s\npack=%s", beforeLogs, afterReadOnlyLogs, readOnly)
	}

	proposalOutput := run("knowledge", "context-pack", "task="+taskKey, "project="+testProjectKey, "--propose", "--json")
	proposalID := extractPacketID(proposalOutput)
	for _, want := range []string{
		`"proposed": true`,
		`"persistence": "review_required"`,
		`"status": "review_required"`,
		`"packet_scope": "operator_context_pack"`,
		`"trigger": "knowledge_context_pack_proposed"`,
	} {
		if !strings.Contains(proposalOutput, want) {
			t.Fatalf("proposal output = %s, want %s", proposalOutput, want)
		}
	}
	listOutput := run("knowledge", "context-packs", "--json")
	if !strings.Contains(listOutput, fmt.Sprintf(`"id": %d`, proposalID)) || !strings.Contains(listOutput, `"status": "review_required"`) {
		t.Fatalf("context-packs list = %s, want review-required proposal %d", listOutput, proposalID)
	}
	reviewList := run("review", "list", "--json")
	queueID := fmt.Sprintf("context-pack:%d", proposalID)
	if !strings.Contains(reviewList, queueID) || !strings.Contains(reviewList, `"source_type": "context_pack"`) || !strings.Contains(reviewList, `"allowed_actions": [
        "accept",
        "reject",
        "archive"
      ]`) {
		t.Fatalf("review list = %s, want context pack review entry", reviewList)
	}
	reviewShow := run("review", "show", queueID, "--json")
	if !strings.Contains(reviewShow, `"object_key": "context-pack-`) || !strings.Contains(reviewShow, `"context_pack"`) || !strings.Contains(reviewShow, taskKey) {
		t.Fatalf("review show = %s, want context pack detail", reviewShow)
	}
	acceptOutput := run("review", "act", queueID, "accept", "--json")
	if !strings.Contains(acceptOutput, `"decision": "accept"`) || !strings.Contains(acceptOutput, `"status": "active"`) {
		t.Fatalf("accept output = %s, want active accepted proposal", acceptOutput)
	}
	repeatAccept := run("review", "act", queueID, "accept", "--json")
	if !strings.Contains(repeatAccept, `"repeated": true`) || !strings.Contains(repeatAccept, `"status": "active"`) {
		t.Fatalf("repeat accept output = %s, want idempotent active proposal", repeatAccept)
	}
	acceptedShow := run("knowledge", "context-pack", "show", fmt.Sprint(proposalID), "--json")
	if !strings.Contains(acceptedShow, `"status": "active"`) || !strings.Contains(acceptedShow, `"review_decision": "accept"`) {
		t.Fatalf("accepted context pack show = %s, want persisted accepted state", acceptedShow)
	}

	rejectOutput := run("knowledge", "context-pack", "task="+taskKey, "project="+testProjectKey, "--propose", "--json")
	rejectID := extractPacketID(rejectOutput)
	rejectQueueID := fmt.Sprintf("context-pack:%d", rejectID)
	rejected := run("review", "act", rejectQueueID, "reject", "--json")
	if !strings.Contains(rejected, `"decision": "reject"`) || !strings.Contains(rejected, `"status": "rejected"`) {
		t.Fatalf("reject output = %s, want rejected proposal", rejected)
	}
	archivedOutput := run("knowledge", "context-pack", "task="+taskKey, "project="+testProjectKey, "--propose", "--json")
	archiveID := extractPacketID(archivedOutput)
	archiveQueueID := fmt.Sprintf("context-pack:%d", archiveID)
	archived := run("review", "act", archiveQueueID, "archive", "--json")
	if !strings.Contains(archived, `"decision": "archive"`) || !strings.Contains(archived, `"status": "archived"`) {
		t.Fatalf("archive output = %s, want archived proposal", archived)
	}

	logs := run("logs", "--json")
	for _, want := range []string{
		`"type": "context_packet.created"`,
		`"type": "context_packet.reviewed"`,
		`"decision": "accept"`,
		`"decision": "reject"`,
		`"decision": "archive"`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs output = %s, want %s", logs, want)
		}
	}
	overview := run("overview", "--json")
	for _, want := range []string{
		`"knowledge_context_packs"`,
		`"review_required_count": 0`,
		`"accepted_count": 1`,
		`"rejected_count": 1`,
		`"archived_count": 1`,
	} {
		if !strings.Contains(overview, want) {
			t.Fatalf("overview output = %s, want %s", overview, want)
		}
	}
	jobs := run("jobs", "--json")
	if strings.Count(jobs, `"task_key"`) != 1 || !strings.Contains(jobs, taskKey) {
		t.Fatalf("jobs output = %s, want no work from context pack review", jobs)
	}
	runs := run("runs", "--json")
	if !strings.Contains(runs, `"runs": []`) {
		t.Fatalf("runs output = %s, want no run creation from context pack review", runs)
	}
}

func TestRunWorkExecuteCompletesDispatchedIntakeRun(t *testing.T) {
	configureLifecycleHarnessDriver(t)
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"prepare weekly summary"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "failed delegation inspection test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "execute test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	if err := Run(context.Background(), root, []string{
		"intake", "raw", "create",
		"--source", "operator",
		"--project", "alpha-cli",
		"--title", "Prepare weekly summary",
		"--type", "request",
		"--dedup-key", "execute-intake",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake raw create) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake process) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"intake", "review", "accept", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake review accept) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"work", "dispatch", "--task", "intake-review-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(work dispatch) error = %v", err)
	}

	var executeOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "execute", "--task", "intake-review-1", "--json"}, strings.NewReader(""), &executeOutput); err != nil {
		t.Fatalf("Run(work execute) error = %v\n%s", err, executeOutput.String())
	}
	var executed struct {
		Executed bool   `json:"executed"`
		Reason   string `json:"reason"`
		Task     struct {
			ID     int64  `json:"id"`
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"task"`
		Run *struct {
			ID      int64  `json:"id"`
			TaskID  int64  `json:"task_id"`
			Status  string `json:"status"`
			Summary string `json:"summary"`
		} `json:"run"`
	}
	if err := json.Unmarshal(executeOutput.Bytes(), &executed); err != nil {
		t.Fatalf("json.Unmarshal(execute) error = %v\n%s", err, executeOutput.String())
	}
	if !executed.Executed || executed.Reason != "completed" || executed.Task.Key != "intake-review-1" || executed.Task.Status != "completed" {
		t.Fatalf("execute output = %+v, want completed task execution", executed)
	}
	if executed.Run == nil || executed.Run.ID != 1 || executed.Run.TaskID != executed.Task.ID || executed.Run.Status != "completed" || executed.Run.Summary != "driver test ok" {
		t.Fatalf("execute run = %+v, want completed run summary", executed.Run)
	}

	var repeatDispatchOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "dispatch", "--task", "intake-review-1", "--json"}, strings.NewReader(""), &repeatDispatchOutput); err != nil {
		t.Fatalf("Run(work dispatch repeat after completion) error = %v", err)
	}
	if output := repeatDispatchOutput.String(); !strings.Contains(output, `"dispatched": false`) || !strings.Contains(output, `"reason": "task_not_queued"`) || strings.Contains(output, `"status": "running"`) {
		t.Fatalf("repeat dispatch output = %s, want safe terminal block", output)
	}

	var runsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"runs", "--json"}, strings.NewReader(""), &runsOutput); err != nil {
		t.Fatalf("Run(runs --json) error = %v", err)
	}
	if output := runsOutput.String(); !strings.Contains(output, `"run_id": 1`) || !strings.Contains(output, `"task_id": 1`) || !strings.Contains(output, `"status": "completed"`) {
		t.Fatalf("runs output = %s, want correlated completed run", output)
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	if output := jobsOutput.String(); !strings.Contains(output, `"task_id": 1`) || !strings.Contains(output, `"status": "completed"`) || strings.Contains(output, `"current_run_id"`) {
		t.Fatalf("jobs output = %s, want completed terminal job without active run", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "run.finished"`,
		`"type": "task.status_changed"`,
		`"status": "completed"`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}

	var statusOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &statusOutput); err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	for _, want := range []string{"work_items=1", "open_work_items=0", "active_run_attempts=0", "dispatch=work_dispatch"} {
		if !strings.Contains(statusOutput.String(), want) {
			t.Fatalf("work status output = %s, want %s", statusOutput.String(), want)
		}
	}
}

func TestRunWorkExecuteBindsDispatchedRunToProjectRoot(t *testing.T) {
	configureLifecycleMetadataEchoDriver(t)
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "project root execution metadata test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	var startOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "start", "--project", testProjectKey, "--title", "Project root metadata proof"}, strings.NewReader(""), &startOutput); err != nil {
		t.Fatalf("Run(work start) error = %v", err)
	}
	taskKey := ""
	for _, field := range strings.Fields(startOutput.String()) {
		if value, ok := strings.CutPrefix(field, "key="); ok {
			taskKey = value
		}
	}
	if taskKey == "" {
		t.Fatalf("work start output = %q, want task key", startOutput.String())
	}

	if err := Run(context.Background(), root, []string{"work", "dispatch", "--task", taskKey, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(work dispatch) error = %v", err)
	}

	var executeOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "execute", "--task", taskKey, "--json"}, strings.NewReader(""), &executeOutput); err != nil {
		t.Fatalf("Run(work execute) error = %v\n%s", err, executeOutput.String())
	}
	var executed struct {
		Run *struct {
			Status  string `json:"status"`
			Summary string `json:"summary"`
		} `json:"run"`
	}
	if err := json.Unmarshal(executeOutput.Bytes(), &executed); err != nil {
		t.Fatalf("json.Unmarshal(execute) error = %v\n%s", err, executeOutput.String())
	}
	wantRoot := filepath.Join(root, "alpha")
	wantSummary := wantRoot + "|" + wantRoot + "|main"
	if executed.Run == nil || executed.Run.Status != "completed" || executed.Run.Summary != wantSummary {
		t.Fatalf("execute run = %+v, want summary %q", executed.Run, wantSummary)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	var detailsJSON string
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT details_json
		FROM run_artifacts
		WHERE run_id = 1 AND artifact_type = 'executor_evidence'
	`).Scan(&detailsJSON); err != nil {
		t.Fatalf("query executor evidence artifact error = %v", err)
	}
	var details map[string]string
	if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
		t.Fatalf("json.Unmarshal(details_json) error = %v\n%s", err, detailsJSON)
	}
	if !strings.HasPrefix(details["artifact_path"], filepath.Join(root, "runs", "artifacts")+string(os.PathSeparator)) {
		t.Fatalf("artifact_path = %q, want runtime-root artifact", details["artifact_path"])
	}
	if strings.HasPrefix(details["artifact_path"], filepath.Join(wantRoot, "runs", "artifacts")+string(os.PathSeparator)) {
		t.Fatalf("artifact_path = %q, should not be written under project repo", details["artifact_path"])
	}
}

func TestRunWorkExecuteSurfacesFailedDispatchedRun(t *testing.T) {
	configureLifecycleHarnessDriverStatus(t, "failed", "driver failed proof")
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"prepare failed dispatch proof"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "failed execute test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	if err := Run(context.Background(), root, []string{
		"intake", "raw", "create",
		"--source", "operator",
		"--project", "alpha-cli",
		"--title", "Prepare failed dispatch proof",
		"--type", "request",
		"--dedup-key", "execute-failed-intake",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake raw create) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"intake", "process", "--id", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake process) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"intake", "review", "accept", "intake-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(intake review accept) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"work", "dispatch", "--task", "intake-review-1", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(work dispatch) error = %v", err)
	}

	var executeOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"work", "execute", "--task", "intake-review-1", "--json"}, strings.NewReader(""), &executeOutput); err != nil {
		t.Fatalf("Run(work execute failed result) error = %v\n%s", err, executeOutput.String())
	}
	if output := executeOutput.String(); !strings.Contains(output, `"executed": true`) || !strings.Contains(output, `"status": "failed"`) || !strings.Contains(output, "driver failed proof") {
		t.Fatalf("execute output = %s, want visible failed execution", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	if output := logsOutput.String(); !strings.Contains(output, `"type": "run.finished"`) || !strings.Contains(output, `"status": "failed"`) || !strings.Contains(output, "driver failed proof") {
		t.Fatalf("logs output = %s, want auditable failed terminal run", output)
	}
}

func TestRunWorkExecuteSurfacesRepoDriverFailure(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")
	t.Setenv("ODIN_CODEX_DRIVER_RUN_RESPONSE", `{"status":"failed","output":"operator visible failure proof"}`)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	installRepoCodexDriverScript(t, root)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"operator visible exact command failure"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "exact command failure test")
	run(
		"intake", "raw", "create",
		"--source", "operator",
		"--project", testProjectKey,
		"--title", "operator visible failure proof",
		"--type", "request",
		"--dedup-key", "exact-command-failure-intake",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	)
	run("intake", "process", "--id", "intake-1", "--json")
	accepted := run("review", "act", "intake-review:1", "accept", "--json")
	if !strings.Contains(accepted, `"work_created": true`) {
		t.Fatalf("accept output = %s, want promoted work", accepted)
	}
	dispatched := run("work", "dispatch", "--task", "intake-review-1", "--json")
	if !strings.Contains(dispatched, `"status": "running"`) {
		t.Fatalf("dispatch output = %s, want running task", dispatched)
	}

	executed := run("work", "execute", "--task", "intake-review-1", "--json")
	if !strings.Contains(executed, `"executed": true`) || !strings.Contains(executed, `"status": "failed"`) || !strings.Contains(executed, "operator visible failure proof") {
		t.Fatalf("execute output = %s, want visible failed execution", executed)
	}

	repeatDispatch := run("work", "dispatch", "--task", "intake-review-1", "--json")
	if !strings.Contains(repeatDispatch, `"dispatched": false`) || !strings.Contains(repeatDispatch, `"reason": "task_not_queued"`) {
		t.Fatalf("repeat dispatch output = %s, want safe non-duplicate dispatch", repeatDispatch)
	}
	repeatExecute := run("work", "execute", "--task", "intake-review-1", "--json")
	if !strings.Contains(repeatExecute, `"executed": false`) || !strings.Contains(repeatExecute, `"reason": "task_not_running"`) {
		t.Fatalf("repeat execute output = %s, want safe non-duplicate execute", repeatExecute)
	}

	runsOutput := run("runs", "--json")
	if !strings.Contains(runsOutput, `"status": "failed"`) {
		t.Fatalf("runs output = %s, want failed terminal run", runsOutput)
	}
	logsOutput := run("logs", "--json")
	if !strings.Contains(logsOutput, `"type": "run.finished"`) || !strings.Contains(logsOutput, `"status": "failed"`) || !strings.Contains(logsOutput, "operator visible failure proof") {
		t.Fatalf("logs output = %s, want auditable failed terminal run", logsOutput)
	}
	statusOutput := run("work", "status")
	if !strings.Contains(statusOutput, "work_items=1") || !strings.Contains(statusOutput, "open_work_items=0") || !strings.Contains(statusOutput, "active_run_attempts=0") {
		t.Fatalf("work status output = %s, want terminal failed work not open or active", statusOutput)
	}
}

func TestRunWorkRetryRequeuesTerminalFailedWorkOnce(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")
	t.Setenv("ODIN_CODEX_DRIVER_RUN_RESPONSE", `{"status":"failed","output":"operator retry failure proof"}`)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	installRepoCodexDriverScript(t, root)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"operator retry proof"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "retry failed work test")
	run(
		"intake", "raw", "create",
		"--source", "operator",
		"--project", testProjectKey,
		"--title", "operator retry failure proof",
		"--type", "request",
		"--dedup-key", "retry-failure-intake",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	)
	run("intake", "process", "--id", "intake-1", "--json")
	run("review", "act", "intake-review:1", "accept", "--json")
	run("work", "dispatch", "--task", "intake-review-1", "--json")
	run("work", "execute", "--task", "intake-review-1", "--json")

	retryOutput := run("work", "retry", "--task", "intake-review-1", "--json")
	if !strings.Contains(retryOutput, `"retried": true`) || !strings.Contains(retryOutput, `"reason": "retried"`) || !strings.Contains(retryOutput, `"status": "queued"`) || !strings.Contains(retryOutput, `"retry_count": 1`) {
		t.Fatalf("retry output = %s, want requeued failed task", retryOutput)
	}
	repeatRetryOutput := run("work", "retry", "--task", "intake-review-1", "--json")
	if !strings.Contains(repeatRetryOutput, `"retried": false`) || !strings.Contains(repeatRetryOutput, `"reason": "already_queued"`) || !strings.Contains(repeatRetryOutput, `"retry_count": 1`) {
		t.Fatalf("repeat retry output = %s, want idempotent already queued", repeatRetryOutput)
	}

	dispatchOutput := run("work", "dispatch", "--task", "intake-review-1", "--json")
	if !strings.Contains(dispatchOutput, `"dispatched": true`) || !strings.Contains(dispatchOutput, `"attempt": 2`) || !strings.Contains(dispatchOutput, `"status": "running"`) {
		t.Fatalf("dispatch output = %s, want new second run attempt", dispatchOutput)
	}
	runsOutput := run("runs", "--json")
	if strings.Count(runsOutput, `"task_key": "intake-review-1"`) != 2 || !strings.Contains(runsOutput, `"attempt": 1`) || !strings.Contains(runsOutput, `"attempt": 2`) || !strings.Contains(runsOutput, `"status": "failed"`) || !strings.Contains(runsOutput, `"status": "running"`) {
		t.Fatalf("runs output = %s, want original failed run plus recovered running attempt", runsOutput)
	}
	logsOutput := run("logs", "--json")
	if !strings.Contains(logsOutput, `"type": "task.queue_state_changed"`) || !strings.Contains(logsOutput, `"retry_count": 1`) {
		t.Fatalf("logs output = %s, want auditable retry queue-state event", logsOutput)
	}
	statusOutput := run("work", "status")
	if !strings.Contains(statusOutput, "work_items=1") || !strings.Contains(statusOutput, "open_work_items=1") || !strings.Contains(statusOutput, "active_run_attempts=1") {
		t.Fatalf("work status output = %s, want active recovered run", statusOutput)
	}
}

func TestRunWorkRetryBlocksAtMaxAttemptsWithGuidance(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")
	t.Setenv("ODIN_CODEX_DRIVER_RUN_RESPONSE", `{"status":"failed","output":"operator retry policy failure proof"}`)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	installRepoCodexDriverScript(t, root)
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"body":"operator retry policy proof"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	run("project", "select", testProjectKey)
	run("transition", "set", "cutover", "confirm", "because", "retry policy test")
	run(
		"intake", "raw", "create",
		"--source", "operator",
		"--project", testProjectKey,
		"--title", "operator retry policy failure proof",
		"--type", "request",
		"--dedup-key", "retry-policy-intake",
		"--requested-by", "codex",
		"--payload-file", payloadPath,
		"--json",
	)
	run("intake", "process", "--id", "intake-1", "--json")
	run("review", "act", "intake-review:1", "accept", "--json")
	for attempt := 1; attempt <= 3; attempt++ {
		dispatchOutput := run("work", "dispatch", "--task", "intake-review-1", "--json")
		if !strings.Contains(dispatchOutput, fmt.Sprintf(`"attempt": %d`, attempt)) || !strings.Contains(dispatchOutput, `"status": "running"`) {
			t.Fatalf("dispatch attempt %d output = %s, want running attempt", attempt, dispatchOutput)
		}
		executeOutput := run("work", "execute", "--task", "intake-review-1", "--json")
		if !strings.Contains(executeOutput, `"status": "failed"`) || !strings.Contains(executeOutput, "operator retry policy failure proof") {
			t.Fatalf("execute attempt %d output = %s, want failed terminal run", attempt, executeOutput)
		}
		if attempt < 3 {
			retryOutput := run("work", "retry", "--task", "intake-review-1", "--json")
			if !strings.Contains(retryOutput, `"retried": true`) || !strings.Contains(retryOutput, `"decision": "retry_allowed"`) || !strings.Contains(retryOutput, fmt.Sprintf(`"retry_count": %d`, attempt)) {
				t.Fatalf("retry attempt %d output = %s, want policy-allowed retry", attempt, retryOutput)
			}
		}
	}

	blockedRetryOutput := run("work", "retry", "--task", "intake-review-1", "--json")
	for _, want := range []string{
		`"retried": false`,
		`"reason": "retry_blocked_max_attempts"`,
		`"decision": "retry_blocked_max_attempts"`,
		`"retry_eligible": false`,
		`"recovery_recommendation": "Open a follow-up or adjust the task before retrying; max attempts reached."`,
		`"status": "failed"`,
		`"retry_count": 2`,
		`"max_attempts": 3`,
	} {
		if !strings.Contains(blockedRetryOutput, want) {
			t.Fatalf("blocked retry output = %s, want %s", blockedRetryOutput, want)
		}
	}
	runsOutput := run("runs", "--json")
	if strings.Count(runsOutput, `"task_key": "intake-review-1"`) != 3 || strings.Count(runsOutput, `"status": "failed"`) < 3 {
		t.Fatalf("runs output = %s, want three failed attempts and no fourth run", runsOutput)
	}
	logsOutput := run("logs", "--json")
	for _, want := range []string{
		`"type": "task.retry_evaluated"`,
		`"decision": "retry_allowed"`,
		`"decision": "retry_blocked_max_attempts"`,
		`"recovery_recommendation": "Open a follow-up or adjust the task before retrying; max attempts reached."`,
	} {
		if !strings.Contains(logsOutput, want) {
			t.Fatalf("logs output = %s, want %s", logsOutput, want)
		}
	}
	overviewOutput := run("overview", "--json")
	if !strings.Contains(overviewOutput, `"recovery_guidance"`) || !strings.Contains(overviewOutput, `"decision": "retry_blocked_max_attempts"`) || !strings.Contains(overviewOutput, `"work_item_key": "intake-review-1"`) {
		t.Fatalf("overview output = %s, want retry guidance for blocked failed work", overviewOutput)
	}
	statusOutput := run("work", "status")
	if !strings.Contains(statusOutput, "failed_retryable_work_items=0") || !strings.Contains(statusOutput, "retry_blocked_work_items=1") {
		t.Fatalf("work status output = %s, want retry policy counts", statusOutput)
	}
}

func TestRunCompanionGetJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, root, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if err := runCompanion(ctx, app, []string{"create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCompanion(create) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runCompanion(ctx, app, []string{"get", "finance", "--json"}, &stdout); err != nil {
		t.Fatalf("runCompanion(get --json) error = %v", err)
	}

	var payload struct {
		Key                string `json:"key"`
		Title              string `json:"title"`
		Kind               string `json:"kind"`
		Status             string `json:"status"`
		ToolPolicyJSON     string `json:"tool_policy_json"`
		MemoryPolicyJSON   string `json:"memory_policy_json"`
		PlanningPolicyJSON string `json:"planning_policy_json"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(get output) error = %v\n%s", err, stdout.String())
	}
	if payload.Key != "finance" {
		t.Fatalf("Key = %q, want finance", payload.Key)
	}
	if payload.Title != "Finance Advisor" {
		t.Fatalf("Title = %q, want Finance Advisor", payload.Title)
	}
	if payload.Kind != "advisor" {
		t.Fatalf("Kind = %q, want advisor", payload.Kind)
	}
	if payload.Status != "active" {
		t.Fatalf("Status = %q, want active", payload.Status)
	}
	if payload.ToolPolicyJSON != "{}" {
		t.Fatalf("ToolPolicyJSON = %q, want {}", payload.ToolPolicyJSON)
	}
	if payload.MemoryPolicyJSON != "{}" {
		t.Fatalf("MemoryPolicyJSON = %q, want {}", payload.MemoryPolicyJSON)
	}
	if payload.PlanningPolicyJSON != "{}" {
		t.Fatalf("PlanningPolicyJSON = %q, want {}", payload.PlanningPolicyJSON)
	}
}

func TestRunCompanionStateJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, root, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if err := runCompanion(ctx, app, []string{"create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCompanion(create) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runCompanion(ctx, app, []string{"state", "finance", "--json"}, &stdout); err != nil {
		t.Fatalf("runCompanion(state --json) error = %v", err)
	}

	var payload struct {
		Key       string `json:"key"`
		Title     string `json:"title"`
		Kind      string `json:"kind"`
		Status    string `json:"status"`
		TaskState struct {
			CompanionKey         string `json:"companion_key"`
			OwnedInitiativeCount int    `json:"owned_initiative_count"`
			OpenWorkItemCount    int    `json:"open_work_item_count"`
			ActiveRunCount       int    `json:"active_run_count"`
			PendingApprovalCount int    `json:"pending_approval_count"`
			BlockedWorkItemCount int    `json:"blocked_work_item_count"`
			OverdueFollowUpCount int    `json:"overdue_follow_up_count"`
		} `json:"task_state"`
		Swarms []struct {
			ParentTaskKey string `json:"parent_task_key"`
			Status        string `json:"status"`
		} `json:"swarms"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(state output) error = %v\n%s", err, stdout.String())
	}
	if payload.Key != "finance" {
		t.Fatalf("Key = %q, want finance", payload.Key)
	}
	if payload.TaskState.CompanionKey != "finance" {
		t.Fatalf("TaskState.CompanionKey = %q, want finance", payload.TaskState.CompanionKey)
	}
	if payload.TaskState.OwnedInitiativeCount != 0 || payload.TaskState.OpenWorkItemCount != 0 || payload.TaskState.ActiveRunCount != 0 || payload.TaskState.PendingApprovalCount != 0 || payload.TaskState.BlockedWorkItemCount != 0 || payload.TaskState.OverdueFollowUpCount != 0 {
		t.Fatalf("TaskState counts = %+v, want zeros for a fresh companion", payload.TaskState)
	}
	if len(payload.Swarms) != 0 {
		t.Fatalf("Swarms len = %d, want 0 for a fresh companion", len(payload.Swarms))
	}
}

func TestRunCompanionCapabilitiesJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, root, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if err := runCompanion(ctx, app, []string{"create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCompanion(create) error = %v", err)
	}

	workspace, err := app.Store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	if _, err := app.Store.DB().ExecContext(ctx, `
		UPDATE companions
		SET tool_policy_json = ?, memory_policy_json = ?, planning_policy_json = ?
		WHERE workspace_id = ? AND key = ?
	`, `{"allow":["branch_proposal","repo_read"]}`, `{"mode":"initiative"}`, `{"mode":"planning","swarm":{"max_children":2}}`, workspace.ID, "finance"); err != nil {
		t.Fatalf("seed companion policy update error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runCompanion(ctx, app, []string{"capabilities", "finance", "--json"}, &stdout); err != nil {
		t.Fatalf("runCompanion(capabilities --json) error = %v", err)
	}

	var payload struct {
		Key        string `json:"key"`
		ToolPolicy struct {
			Allow []string `json:"allow"`
		} `json:"tool_policy"`
		MemoryPolicy struct {
			Mode string `json:"mode"`
		} `json:"memory_policy"`
		PlanningPolicy struct {
			Mode  string `json:"mode"`
			Swarm struct {
				MaxChildren int `json:"max_children"`
			} `json:"swarm"`
		} `json:"planning_policy"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(capabilities output) error = %v\n%s", err, stdout.String())
	}
	if payload.Key != "finance" {
		t.Fatalf("Key = %q, want finance", payload.Key)
	}
	if len(payload.ToolPolicy.Allow) != 2 || payload.ToolPolicy.Allow[0] != "branch_proposal" || payload.ToolPolicy.Allow[1] != "repo_read" {
		t.Fatalf("ToolPolicy.Allow = %+v, want branch_proposal and repo_read", payload.ToolPolicy.Allow)
	}
	if payload.MemoryPolicy.Mode != "initiative" {
		t.Fatalf("MemoryPolicy.Mode = %q, want initiative", payload.MemoryPolicy.Mode)
	}
	if payload.PlanningPolicy.Mode != "planning" {
		t.Fatalf("PlanningPolicy.Mode = %q, want planning", payload.PlanningPolicy.Mode)
	}
	if payload.PlanningPolicy.Swarm.MaxChildren != 2 {
		t.Fatalf("PlanningPolicy.Swarm.MaxChildren = %d, want 2", payload.PlanningPolicy.Swarm.MaxChildren)
	}
}

func TestRunCompanionRejectsUnsupportedSubcommand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, root, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if err := runCompanion(ctx, app, []string{"delete", "finance"}, &bytes.Buffer{}); err == nil {
		t.Fatal("runCompanion(delete) error = nil, want unsupported companion subcommand error")
	}
}

func TestCompanionRunCreatesOwnedTaskInDefaultScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := testRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, root, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	if err := runCompanion(ctx, app, []string{"create", "--kind", "advisor", "--key", "finance", "--title", "Finance Advisor"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCompanion(create) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := runCompanion(ctx, app, []string{"run", "finance", "--objective", "review April budget", "--json"}, &stdout); err != nil {
		t.Fatalf("runCompanion(run --json) error = %v\n%s", err, stdout.String())
	}

	var payload struct {
		CompanionKey          string `json:"companion_key"`
		Objective             string `json:"objective"`
		RequestedSwarmTrigger string `json:"requested_swarm_trigger,omitempty"`
		Task                  struct {
			ID     int64  `json:"id"`
			Key    string `json:"key"`
			Status string `json:"status"`
			Scope  string `json:"scope"`
		} `json:"task"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(runCompanion output) error = %v\n%s", err, stdout.String())
	}
	if payload.CompanionKey != "finance" {
		t.Fatalf("CompanionKey = %q, want finance", payload.CompanionKey)
	}
	if payload.Objective != "review April budget" {
		t.Fatalf("Objective = %q, want review April budget", payload.Objective)
	}
	if payload.RequestedSwarmTrigger != "" {
		t.Fatalf("RequestedSwarmTrigger = %q, want empty", payload.RequestedSwarmTrigger)
	}
	if payload.Task.Status != "queued" {
		t.Fatalf("Task.Status = %q, want queued", payload.Task.Status)
	}

	task, err := app.Store.GetTask(ctx, payload.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.CompanionID == nil {
		t.Fatal("Task.CompanionID = nil, want companion ownership")
	}
	if task.RequestedBy != "companion" {
		t.Fatalf("Task.RequestedBy = %q, want companion", task.RequestedBy)
	}
	if task.ActionKey != "" {
		t.Fatalf("Task.ActionKey = %q, want empty without trigger", task.ActionKey)
	}

	project, err := app.Store.GetProjectByKey(ctx, "odin-core")
	if err != nil {
		t.Fatalf("GetProjectByKey(odin-core) error = %v", err)
	}
	if task.ProjectID != project.ID {
		t.Fatalf("Task.ProjectID = %d, want odin-core project %d", task.ProjectID, project.ID)
	}
	if task.WorkspaceID == nil {
		t.Fatal("Task.WorkspaceID = nil, want workspace ownership")
	}
	if task.InitiativeID == nil {
		t.Fatal("Task.InitiativeID = nil, want initiative ownership")
	}
}

func TestCompanionDelegateCreatesAuditableChildWork(t *testing.T) {
	configureLifecycleHarnessDriver(t)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	seedDelegationSkillFixture(t, root)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "delegation inspection test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	var output bytes.Buffer
	if err := Run(context.Background(), root, []string{
		"companion",
		"delegate",
		"primary",
		"--agent",
		"portal-delivery-agent",
		"--portal-track",
		"admin",
		"--surface",
		"dashboard",
		"--goal",
		"audit delegated operator path",
		"--json",
	}, strings.NewReader(""), &output); err != nil {
		t.Fatalf("Run(companion delegate) error = %v\nstdout:\n%s", err, output.String())
	}
	for _, want := range []string{
		`"companion_key": "primary"`,
		`"agent_key": "portal-delivery-agent"`,
		`"parent_task"`,
		`"parent_run"`,
		`"child_delegations"`,
		`"delegation_key": "ia-audit"`,
		`"child_task_id":`,
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("delegate output = %s, want %s", output.String(), want)
		}
	}
	var delegatePayload struct {
		ChildDelegations []struct {
			ID            int64  `json:"id"`
			DelegationKey string `json:"delegation_key"`
		} `json:"child_delegations"`
	}
	if err := json.Unmarshal(output.Bytes(), &delegatePayload); err != nil {
		t.Fatalf("delegate json = %v\n%s", err, output.String())
	}
	if len(delegatePayload.ChildDelegations) == 0 {
		t.Fatalf("child delegations len = 0, want delegated children")
	}

	var listOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "list", "--json"}, strings.NewReader(""), &listOutput); err != nil {
		t.Fatalf("Run(companion delegate list --json) error = %v\nstdout:\n%s", err, listOutput.String())
	}
	for _, want := range []string{
		`"delegations"`,
		`"delegation_key": "ia-audit"`,
		`"parent_task_id":`,
		`"parent_run_id":`,
		`"child_task_id":`,
		`"artifact_count":`,
	} {
		if !strings.Contains(listOutput.String(), want) {
			t.Fatalf("delegate list output = %s, want %s", listOutput.String(), want)
		}
	}

	var showOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "show", strconv.FormatInt(delegatePayload.ChildDelegations[0].ID, 10), "--json"}, strings.NewReader(""), &showOutput); err != nil {
		t.Fatalf("Run(companion delegate show --json) error = %v\nstdout:\n%s", err, showOutput.String())
	}
	for _, want := range []string{
		`"delegation"`,
		`"artifacts"`,
		`"artifact_type": "run_summary"`,
		`"details_json":`,
	} {
		if !strings.Contains(showOutput.String(), want) {
			t.Fatalf("delegate show output = %s, want %s", showOutput.String(), want)
		}
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "delegation.created"`,
		`"type": "delegation.child_attached"`,
		`"type": "delegation.status_changed"`,
		`"type": "delegation.artifact_recorded"`,
		`"parent_task_id":`,
		`"child_task_id":`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}

	var overviewOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"overview", "--json"}, strings.NewReader(""), &overviewOutput); err != nil {
		t.Fatalf("Run(overview --json) error = %v", err)
	}
	for _, want := range []string{
		`"companion_swarms"`,
		`"delegation_count": 5`,
		`"completed_delegation_count": 5`,
		`"blocked_work_item_count": 0`,
		`"runtime_status": "delegation_artifacts_visible"`,
		`"operator_surface": "companion delegate"`,
	} {
		if !strings.Contains(overviewOutput.String(), want) {
			t.Fatalf("overview output = %s, want %s", overviewOutput.String(), want)
		}
	}
}

func TestCompanionDelegateCreateIsIdempotentForSameLogicalRequest(t *testing.T) {
	configureLifecycleHarnessDriver(t)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	seedDelegationSkillFixture(t, root)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "delegation idempotency test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	args := []string{
		"companion",
		"delegate",
		"primary",
		"--agent",
		"portal-delivery-agent",
		"--portal-track",
		"admin",
		"--surface",
		"dashboard",
		"--goal",
		"audit idempotent delegated operator path",
		"--json",
	}
	var firstOutput bytes.Buffer
	if err := Run(context.Background(), root, args, strings.NewReader(""), &firstOutput); err != nil {
		t.Fatalf("Run(companion delegate first) error = %v\nstdout:\n%s", err, firstOutput.String())
	}
	var secondOutput bytes.Buffer
	if err := Run(context.Background(), root, args, strings.NewReader(""), &secondOutput); err != nil {
		t.Fatalf("Run(companion delegate repeat) error = %v\nstdout:\n%s", err, secondOutput.String())
	}

	var firstPayload struct {
		Reused     bool `json:"reused"`
		ParentTask struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		} `json:"parent_task"`
		ParentRun *struct {
			RunID int64 `json:"run_id"`
		} `json:"parent_run"`
		ChildDelegations []struct {
			ID          int64  `json:"id"`
			Status      string `json:"status"`
			ChildTaskID int64  `json:"child_task_id"`
			ChildRunID  *int64 `json:"child_run_id,omitempty"`
		} `json:"child_delegations"`
	}
	var secondPayload struct {
		Reused     bool   `json:"reused"`
		Reason     string `json:"reason"`
		ParentTask struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		} `json:"parent_task"`
		ParentRun *struct {
			RunID int64 `json:"run_id"`
		} `json:"parent_run"`
		ChildDelegations []struct {
			ID          int64  `json:"id"`
			Status      string `json:"status"`
			ChildTaskID int64  `json:"child_task_id"`
			ChildRunID  *int64 `json:"child_run_id,omitempty"`
		} `json:"child_delegations"`
	}
	if err := json.Unmarshal(firstOutput.Bytes(), &firstPayload); err != nil {
		t.Fatalf("first delegate json = %v\n%s", err, firstOutput.String())
	}
	if err := json.Unmarshal(secondOutput.Bytes(), &secondPayload); err != nil {
		t.Fatalf("second delegate json = %v\n%s", err, secondOutput.String())
	}
	if firstPayload.Reused {
		t.Fatal("first delegate reused = true, want fresh create")
	}
	if !secondPayload.Reused || secondPayload.Reason != "existing_delegation_tree" {
		t.Fatalf("second delegate reused=%t reason=%q, want existing_delegation_tree", secondPayload.Reused, secondPayload.Reason)
	}
	if firstPayload.ParentTask.ID != secondPayload.ParentTask.ID || firstPayload.ParentTask.Key != secondPayload.ParentTask.Key {
		t.Fatalf("repeat parent = (%d,%s), want same (%d,%s)", secondPayload.ParentTask.ID, secondPayload.ParentTask.Key, firstPayload.ParentTask.ID, firstPayload.ParentTask.Key)
	}
	if firstPayload.ParentRun == nil || secondPayload.ParentRun == nil || firstPayload.ParentRun.RunID != secondPayload.ParentRun.RunID {
		t.Fatalf("repeat parent run = %+v, want same %+v", secondPayload.ParentRun, firstPayload.ParentRun)
	}
	if len(firstPayload.ChildDelegations) != 5 || len(secondPayload.ChildDelegations) != 5 {
		t.Fatalf("child delegation counts = first %d second %d, want 5 each", len(firstPayload.ChildDelegations), len(secondPayload.ChildDelegations))
	}
	for index := range firstPayload.ChildDelegations {
		first := firstPayload.ChildDelegations[index]
		second := secondPayload.ChildDelegations[index]
		if first.ID != second.ID || first.ChildTaskID != second.ChildTaskID {
			t.Fatalf("child %d repeat = %+v, want same id/task as %+v", index, second, first)
		}
		if first.ChildRunID == nil || second.ChildRunID == nil || *first.ChildRunID != *second.ChildRunID {
			t.Fatalf("child %d repeat run = %+v, want same %+v", index, second.ChildRunID, first.ChildRunID)
		}
	}

	var listOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "list", "--json"}, strings.NewReader(""), &listOutput); err != nil {
		t.Fatalf("Run(companion delegate list --json) error = %v", err)
	}
	var listPayload struct {
		Delegations []struct {
			ID int64 `json:"id"`
		} `json:"delegations"`
	}
	if err := json.Unmarshal(listOutput.Bytes(), &listPayload); err != nil {
		t.Fatalf("list json = %v\n%s", err, listOutput.String())
	}
	if len(listPayload.Delegations) != 5 {
		t.Fatalf("delegation rows after repeat = %d, want 5\n%s", len(listPayload.Delegations), listOutput.String())
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	if !strings.Contains(logsOutput.String(), `"type": "delegation.create_reused"`) || !strings.Contains(logsOutput.String(), `"reason": "existing_delegation_tree"`) {
		t.Fatalf("logs output = %s, want delegation.create_reused evidence", logsOutput.String())
	}
}

func TestCompanionDelegateGovernanceIntentRequiresApproval(t *testing.T) {
	configureLifecycleHarnessDriver(t)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	seedDelegationSkillFixture(t, root)
	if err := Run(context.Background(), root, []string{"project", "select", "odin-core"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select odin-core) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "delegation governance intent test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	var output bytes.Buffer
	err := Run(context.Background(), root, []string{
		"companion",
		"delegate",
		"primary",
		"--agent",
		"portal-delivery-agent",
		"--portal-track",
		"odin-core",
		"--surface",
		"policy",
		"--goal",
		"audit approval-aware delegated governance work",
		"--intent",
		"governance",
		"--json",
	}, strings.NewReader(""), &output)
	if err == nil {
		t.Fatalf("Run(companion delegate governance) error = nil, want approval-gated delegation\nstdout:\n%s", output.String())
	}

	var listOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "list", "--json"}, strings.NewReader(""), &listOutput); err != nil {
		t.Fatalf("Run(companion delegate list --json) error = %v\nstdout:\n%s", err, listOutput.String())
	}
	for _, want := range []string{
		`"status": "blocked"`,
		`"mutation_mode": "governance"`,
		`"execution_intent": "governance"`,
		`"execution_intent_source": "companion_delegate"`,
	} {
		if !strings.Contains(listOutput.String(), want) {
			t.Fatalf("delegate list output = %s, want %s", listOutput.String(), want)
		}
	}
	var listPayload struct {
		Delegations []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"delegations"`
	}
	if err := json.Unmarshal(listOutput.Bytes(), &listPayload); err != nil {
		t.Fatalf("delegate list json = %v\n%s", err, listOutput.String())
	}
	if len(listPayload.Delegations) == 0 {
		t.Fatalf("delegate list = %s, want blocked delegation rows", listOutput.String())
	}

	var retryOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "retry", strconv.FormatInt(listPayload.Delegations[0].ID, 10), "--json"}, strings.NewReader(""), &retryOutput); err != nil {
		t.Fatalf("Run(companion delegate retry approval-blocked) error = %v\nstdout:\n%s", err, retryOutput.String())
	}
	for _, want := range []string{
		`"retried": false`,
		`"reason": "approval_required"`,
		`"status": "blocked"`,
		`"execution_intent": "governance"`,
	} {
		if !strings.Contains(retryOutput.String(), want) {
			t.Fatalf("retry output = %s, want %s", retryOutput.String(), want)
		}
	}

	var approvalsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"approvals", "all", "--json"}, strings.NewReader(""), &approvalsOutput); err != nil {
		t.Fatalf("Run(approvals all --json) error = %v", err)
	}
	if output := approvalsOutput.String(); !strings.Contains(output, `"status": "pending"`) || !strings.Contains(output, `"task_key": "odin-core-policy-`) {
		t.Fatalf("approvals output = %s, want pending approval for delegated governance child", output)
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	for _, want := range []string{
		`"status": "blocked"`,
		`"blocked_reason": "approval_required"`,
		`"execution_intent": "governance"`,
		`"execution_intent_source": "companion_delegate"`,
	} {
		if !strings.Contains(jobsOutput.String(), want) {
			t.Fatalf("jobs output = %s, want %s", jobsOutput.String(), want)
		}
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "delegation.created"`,
		`"mutation_mode": "governance"`,
		`"type": "approval.requested"`,
		`"type": "task.status_changed"`,
		`"status": "blocked"`,
		`"execution_intent": "governance"`,
		`"execution_intent_source": "companion_delegate"`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}
}

func TestCompanionDelegateListShowsFailedPartialLifecycle(t *testing.T) {
	configureLifecycleHarnessDriverStatus(t, "failed", "delegated child failed proof")
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	seedDelegationSkillFixture(t, root)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}

	var output bytes.Buffer
	err := Run(context.Background(), root, []string{
		"companion",
		"delegate",
		"primary",
		"--agent",
		"portal-delivery-agent",
		"--portal-track",
		"admin",
		"--surface",
		"dashboard",
		"--goal",
		"audit failed delegated operator path",
		"--json",
	}, strings.NewReader(""), &output)
	if err == nil {
		t.Fatalf("Run(companion delegate) error = nil, want failed child execution\nstdout:\n%s", output.String())
	}

	var listOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "list", "--json"}, strings.NewReader(""), &listOutput); err != nil {
		t.Fatalf("Run(companion delegate list --json) error = %v\nstdout:\n%s", err, listOutput.String())
	}
	for _, want := range []string{
		`"delegations"`,
		`"status": "failed"`,
		`"child_task_id":`,
		`"artifact_count":`,
	} {
		if !strings.Contains(listOutput.String(), want) {
			t.Fatalf("delegate list output = %s, want %s", listOutput.String(), want)
		}
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "delegation.status_changed"`,
		`"status": "failed"`,
		`delegated child failed proof`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}
}

func TestCompanionDelegateRetryRecoversFailedChildrenIdempotently(t *testing.T) {
	configureLifecycleHarnessDriverStatus(t, "failed", "delegated child failed proof")
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	seedDelegationSkillFixture(t, root)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}

	var failedOutput bytes.Buffer
	err := Run(context.Background(), root, []string{
		"companion",
		"delegate",
		"primary",
		"--agent",
		"portal-delivery-agent",
		"--portal-track",
		"admin",
		"--surface",
		"dashboard",
		"--goal",
		"audit failed delegated operator path",
		"--json",
	}, strings.NewReader(""), &failedOutput)
	if err == nil {
		t.Fatalf("Run(companion delegate) error = nil, want failed child execution\nstdout:\n%s", failedOutput.String())
	}

	var failedListOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "list", "--json"}, strings.NewReader(""), &failedListOutput); err != nil {
		t.Fatalf("Run(companion delegate list failed) error = %v", err)
	}
	var failedList struct {
		Delegations []struct {
			ID          int64  `json:"id"`
			Status      string `json:"status"`
			ChildTaskID int64  `json:"child_task_id"`
			ChildRunID  *int64 `json:"child_run_id,omitempty"`
		} `json:"delegations"`
	}
	if err := json.Unmarshal(failedListOutput.Bytes(), &failedList); err != nil {
		t.Fatalf("failed list json = %v\n%s", err, failedListOutput.String())
	}
	if len(failedList.Delegations) != 2 {
		t.Fatalf("failed delegation count = %d, want 2\n%s", len(failedList.Delegations), failedListOutput.String())
	}
	for _, delegation := range failedList.Delegations {
		if delegation.Status != "failed" || delegation.ChildTaskID == 0 || delegation.ChildRunID == nil {
			t.Fatalf("failed delegation = %+v, want failed child task with failed child run", delegation)
		}
	}

	var repeatFailedOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{
		"companion",
		"delegate",
		"primary",
		"--agent",
		"portal-delivery-agent",
		"--portal-track",
		"admin",
		"--surface",
		"dashboard",
		"--goal",
		"audit failed delegated operator path",
		"--json",
	}, strings.NewReader(""), &repeatFailedOutput); err != nil {
		t.Fatalf("Run(companion delegate repeat failed) error = %v\nstdout:\n%s", err, repeatFailedOutput.String())
	}
	var repeatFailedPayload struct {
		Reused           bool   `json:"reused"`
		Reason           string `json:"reason"`
		ChildDelegations []struct {
			ID          int64  `json:"id"`
			Status      string `json:"status"`
			ChildTaskID int64  `json:"child_task_id"`
			ChildRunID  *int64 `json:"child_run_id,omitempty"`
		} `json:"child_delegations"`
	}
	if err := json.Unmarshal(repeatFailedOutput.Bytes(), &repeatFailedPayload); err != nil {
		t.Fatalf("repeat failed json = %v\n%s", err, repeatFailedOutput.String())
	}
	if !repeatFailedPayload.Reused || repeatFailedPayload.Reason != "existing_failed_use_retry" {
		t.Fatalf("repeat failed reused=%t reason=%q, want existing_failed_use_retry", repeatFailedPayload.Reused, repeatFailedPayload.Reason)
	}
	if len(repeatFailedPayload.ChildDelegations) != len(failedList.Delegations) {
		t.Fatalf("repeat failed child count = %d, want %d", len(repeatFailedPayload.ChildDelegations), len(failedList.Delegations))
	}
	failedByID := make(map[int64]int64, len(failedList.Delegations))
	for _, delegation := range failedList.Delegations {
		failedByID[delegation.ID] = delegation.ChildTaskID
	}
	for index, delegation := range repeatFailedPayload.ChildDelegations {
		originalChildTaskID, ok := failedByID[delegation.ID]
		if !ok || delegation.ChildTaskID != originalChildTaskID || delegation.Status != "failed" || delegation.ChildRunID == nil {
			t.Fatalf("repeat failed delegation %d = %+v, want same failed row set %#v", index, delegation, failedByID)
		}
	}
	var repeatFailedListOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "list", "--json"}, strings.NewReader(""), &repeatFailedListOutput); err != nil {
		t.Fatalf("Run(companion delegate list repeat failed) error = %v", err)
	}
	if strings.Count(repeatFailedListOutput.String(), `"status": "failed"`) != 2 {
		t.Fatalf("repeat failed list = %s, want exactly two failed delegation rows", repeatFailedListOutput.String())
	}

	configureLifecycleHarnessDriver(t)
	if err := Run(context.Background(), root, []string{"transition", "set", "cutover", "confirm", "because", "delegation retry test"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	firstID := strconv.FormatInt(failedList.Delegations[0].ID, 10)
	var retryOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "retry", firstID, "--json"}, strings.NewReader(""), &retryOutput); err != nil {
		t.Fatalf("Run(companion delegate retry first) error = %v\nstdout:\n%s", err, retryOutput.String())
	}
	var retryPayload struct {
		Retried    bool   `json:"retried"`
		Reason     string `json:"reason"`
		Delegation struct {
			ID         int64  `json:"id"`
			Status     string `json:"status"`
			ChildRunID *int64 `json:"child_run_id,omitempty"`
		} `json:"delegation"`
	}
	if err := json.Unmarshal(retryOutput.Bytes(), &retryPayload); err != nil {
		t.Fatalf("retry json = %v\n%s", err, retryOutput.String())
	}
	if !retryPayload.Retried || retryPayload.Reason != "retried" || retryPayload.Delegation.Status != "completed" || retryPayload.Delegation.ChildRunID == nil {
		t.Fatalf("retry payload = %+v, want completed retried delegation with child run", retryPayload)
	}
	firstRunID := *retryPayload.Delegation.ChildRunID

	var repeatRetryOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "retry", firstID, "--json"}, strings.NewReader(""), &repeatRetryOutput); err != nil {
		t.Fatalf("Run(companion delegate retry repeat) error = %v\nstdout:\n%s", err, repeatRetryOutput.String())
	}
	var repeatRetryPayload struct {
		Retried    bool   `json:"retried"`
		Reason     string `json:"reason"`
		Delegation struct {
			Status     string `json:"status"`
			ChildRunID *int64 `json:"child_run_id,omitempty"`
		} `json:"delegation"`
	}
	if err := json.Unmarshal(repeatRetryOutput.Bytes(), &repeatRetryPayload); err != nil {
		t.Fatalf("repeat retry json = %v\n%s", err, repeatRetryOutput.String())
	}
	if repeatRetryPayload.Retried || repeatRetryPayload.Reason != "already_completed" || repeatRetryPayload.Delegation.ChildRunID == nil || *repeatRetryPayload.Delegation.ChildRunID != firstRunID {
		t.Fatalf("repeat retry payload = %+v, want idempotent already_completed with same child run %d", repeatRetryPayload, firstRunID)
	}

	secondID := strconv.FormatInt(failedList.Delegations[1].ID, 10)
	var secondRetryOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "retry", secondID, "--json"}, strings.NewReader(""), &secondRetryOutput); err != nil {
		t.Fatalf("Run(companion delegate retry second) error = %v\nstdout:\n%s", err, secondRetryOutput.String())
	}

	var recoveredListOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"companion", "delegate", "list", "--json"}, strings.NewReader(""), &recoveredListOutput); err != nil {
		t.Fatalf("Run(companion delegate list recovered) error = %v", err)
	}
	var recoveredList struct {
		Delegations []struct {
			Status      string `json:"status"`
			ChildTaskID int64  `json:"child_task_id"`
			ChildRunID  *int64 `json:"child_run_id,omitempty"`
		} `json:"delegations"`
	}
	if err := json.Unmarshal(recoveredListOutput.Bytes(), &recoveredList); err != nil {
		t.Fatalf("recovered list json = %v\n%s", err, recoveredListOutput.String())
	}
	if len(recoveredList.Delegations) != 2 {
		t.Fatalf("recovered delegation count = %d, want same 2 rows\n%s", len(recoveredList.Delegations), recoveredListOutput.String())
	}
	for _, delegation := range recoveredList.Delegations {
		if delegation.Status != "completed" || delegation.ChildTaskID == 0 || delegation.ChildRunID == nil {
			t.Fatalf("recovered delegation = %+v, want completed linked child task/run", delegation)
		}
	}

	var overviewOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"overview", "--json"}, strings.NewReader(""), &overviewOutput); err != nil {
		t.Fatalf("Run(overview --json) error = %v", err)
	}
	for _, want := range []string{
		`"status": "completed"`,
		`"delegation_count": 2`,
		`"completed_delegation_count": 2`,
		`"backlog_count": 0`,
	} {
		if !strings.Contains(overviewOutput.String(), want) {
			t.Fatalf("overview output = %s, want %s", overviewOutput.String(), want)
		}
	}

	var runsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"runs", "--json"}, strings.NewReader(""), &runsOutput); err != nil {
		t.Fatalf("Run(runs --json) error = %v", err)
	}
	if output := runsOutput.String(); strings.Count(output, `"status": "completed"`) != 3 {
		t.Fatalf("runs output = %s, want parent plus two recovered child runs completed", output)
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "delegation.retry_requested"`,
		`"type": "delegation.retry_skipped"`,
		`"reason": "already_completed"`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}
}

func TestRunDoctorIgnoresInvalidOdinNowForNonAgendaCommands(t *testing.T) {
	root := testRepoRoot(t)
	t.Setenv("ODIN_NOW", "definitely-not-a-timestamp")

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"doctor", "--json"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run(doctor --json) error = %v", err)
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("doctor json = %v\n%s", err, stdout.String())
	}
	if payload.Status == "" {
		t.Fatalf("Status = %q, want non-empty", payload.Status)
	}
}

func TestRunProjectListText(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"project", "list"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "odin-core") {
		t.Fatalf("stdout = %q, want project key", stdout.String())
	}
}

func TestRunProjectSelectPersistsSession(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "project="+testProjectKey) {
		t.Fatalf("stdout = %q, want selection confirmation", stdout.String())
	}

	sessionBytes, err := os.ReadFile(filepath.Join(root, "state", "cache", "cli-session.json"))
	if err != nil {
		t.Fatalf("ReadFile(cli-session.json) error = %v", err)
	}
	if !strings.Contains(string(sessionBytes), "\"project_key\": \""+testProjectKey+"\"") {
		t.Fatalf("session = %q, want alpha project selection", string(sessionBytes))
	}
}

func TestRunApprovalsResolveUnsupportedApproveDoesNotMutate(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	approvalID, taskID, prepareRunID := seedPendingApprovalRuntime(t, root)

	var stdout bytes.Buffer
	args := []string{"approvals", "resolve", fmt.Sprintf("%d", approvalID), "approve", "final", "confirmation"}
	if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(%v) error = %v", args, err)
	}

	output := stdout.String()
	for _, want := range []string{
		fmt.Sprintf("approval=%d", approvalID),
		"status=unsupported",
		"result=not_resolved",
		"summary=approval has no registered resolver; inspect only",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want substring %q", output, want)
		}
	}
	if strings.Contains(output, "final confirmation") {
		t.Fatalf("output = %q, want compact output without echoed reason", output)
	}
	if strings.Contains(output, "run=") {
		t.Fatalf("output = %q, want no run handle for unsupported approval", output)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	approval, err := store.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if approval.Status != "pending" {
		t.Fatalf("approval.Status = %q, want %q", approval.Status, "pending")
	}

	runIDs := listRuntimeTaskRunIDs(t, root, taskID)
	if len(runIDs) != 1 || runIDs[0] != prepareRunID {
		t.Fatalf("task run ids = %v, want only prepare run %d", runIDs, prepareRunID)
	}
}

func TestRunApprovalsResolveUnsupportedDenyDoesNotMutate(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	approvalID, taskID, prepareRunID := seedPendingApprovalRuntime(t, root)

	var stdout bytes.Buffer
	args := []string{"approvals", "resolve", fmt.Sprintf("%d", approvalID), "deny", "amount", "is", "wrong"}
	if err := Run(context.Background(), root, args, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(%v) error = %v", args, err)
	}

	output := stdout.String()
	for _, want := range []string{
		fmt.Sprintf("approval=%d", approvalID),
		"status=unsupported",
		"result=not_resolved",
		"summary=approval has no registered resolver; inspect only",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want substring %q", output, want)
		}
	}
	if strings.Contains(output, "run=") {
		t.Fatalf("output = %q, want no run handle on deny", output)
	}
	if strings.Contains(output, "amount is wrong") {
		t.Fatalf("output = %q, want compact output without echoed reason", output)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	approval, err := store.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if approval.Status != "pending" {
		t.Fatalf("approval.Status = %q, want %q", approval.Status, "pending")
	}

	runIDs := listRuntimeTaskRunIDs(t, root, taskID)
	if len(runIDs) != 1 || runIDs[0] != prepareRunID {
		t.Fatalf("task run ids = %v, want only prepare run %d", runIDs, prepareRunID)
	}
}

func TestRunApprovalsResolveTaskBackedCompanionWork(t *testing.T) {
	configureLifecycleHarnessDriver(t)
	t.Setenv("HOME", t.TempDir())

	root := testRepoRoot(t)
	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	run("project", "select", "odin-core")
	run("transition", "set", "cutover", "confirm", "because", "task-backed approval test")
	run("companion", "run", "primary", "--objective", "Prepare companion approval proof", "--trigger", "test", "--json")

	blocked := run("work", "dispatch", "--task", "1", "--json")
	if !strings.Contains(blocked, `"dispatched": false`) || !strings.Contains(blocked, `"reason": "approval_required"`) || !strings.Contains(blocked, `"status": "blocked"`) {
		t.Fatalf("dispatch output = %s, want approval-required block", blocked)
	}

	approvalList := run("approvals", "all", "--json")
	for _, want := range []string{
		`"approval_id": 1`,
		`"resolver_support": "supported"`,
		`"source": "approval_requests"`,
		`"risk": "governance"`,
		`"reason": "approval_required"`,
		`"allowed_actions": [`,
		`"approve"`,
		`"next_steps": "inspect with odin approvals show 1; resolve with odin approvals resolve 1 \u003capprove|deny\u003e \u003creason...\u003e"`,
		`"on_approve": "task unblocked or registered continuation starts"`,
	} {
		if !strings.Contains(approvalList, want) {
			t.Fatalf("approval list output = %s, want %s", approvalList, want)
		}
	}

	show := run("approvals", "show", "1", "--json")
	for _, want := range []string{
		`"id": 1`,
		`"status": "pending"`,
		`"task_id": 1`,
		`"task_status": "blocked"`,
		`"resolver_support": "supported"`,
		`"source": "approval_requests"`,
		`"risk": "governance"`,
		`"reason": "approval_required"`,
		`"allowed_actions": [`,
		`"approve"`,
		`"next_steps": "inspect with odin approvals show 1; resolve with odin approvals resolve 1 \u003capprove|deny\u003e \u003creason...\u003e"`,
		`"on_approve": "task unblocked or registered continuation starts"`,
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("approval show output = %s, want %s", show, want)
		}
	}

	approved := run("approvals", "resolve", "1", "approve", "operator", "approved", "task", "work", "--json")
	for _, want := range []string{
		`"id": 1`,
		`"status": "approved"`,
		`"resolver_support": "supported"`,
		`"result": "approved"`,
		`"summary": "approval granted; task unblocked"`,
	} {
		if !strings.Contains(approved, want) {
			t.Fatalf("approval resolve output = %s, want %s", approved, want)
		}
	}

	repeatApproved := run("approvals", "resolve", "1", "approve", "repeat", "approval", "--json")
	if !strings.Contains(repeatApproved, `"status": "approved"`) || !strings.Contains(repeatApproved, `"result": "approved"`) {
		t.Fatalf("repeat approval output = %s, want idempotent approved result", repeatApproved)
	}

	jobsAfterApprove := run("jobs", "--json")
	if !strings.Contains(jobsAfterApprove, `"task_id": 1`) || !strings.Contains(jobsAfterApprove, `"status": "queued"`) {
		t.Fatalf("jobs after approve = %s, want task requeued", jobsAfterApprove)
	}

	dispatched := run("work", "dispatch", "--task", "1", "--json")
	if !strings.Contains(dispatched, `"dispatched": true`) || !strings.Contains(dispatched, `"status": "running"`) {
		t.Fatalf("dispatch after approve = %s, want running run", dispatched)
	}
	executed := run("work", "execute", "--task", "1", "--json")
	if !strings.Contains(executed, `"executed": true`) || !strings.Contains(executed, `"reason": "completed"`) || !strings.Contains(executed, `"status": "completed"`) {
		t.Fatalf("execute after approve = %s, want completed task", executed)
	}

	run("companion", "run", "primary", "--objective", "Prepare companion denial proof", "--trigger", "test", "--json")
	denialBlock := run("work", "dispatch", "--task", "2", "--json")
	if !strings.Contains(denialBlock, `"reason": "approval_required"`) {
		t.Fatalf("denial dispatch output = %s, want approval-required block", denialBlock)
	}
	denied := run("approvals", "resolve", "2", "deny", "operator", "denied", "task", "work", "--json")
	if !strings.Contains(denied, `"status": "denied"`) || !strings.Contains(denied, `"result": "denied"`) {
		t.Fatalf("deny output = %s, want denied result", denied)
	}
	repeatDenied := run("approvals", "resolve", "2", "deny", "repeat", "denial", "--json")
	if !strings.Contains(repeatDenied, `"status": "denied"`) || !strings.Contains(repeatDenied, `"result": "denied"`) {
		t.Fatalf("repeat deny output = %s, want idempotent denied result", repeatDenied)
	}
	deniedDispatch := run("work", "dispatch", "--task", "2", "--json")
	if !strings.Contains(deniedDispatch, `"dispatched": false`) || !strings.Contains(deniedDispatch, `"reason": "task_not_queued"`) || strings.Contains(deniedDispatch, `"status": "running"`) {
		t.Fatalf("denied dispatch output = %s, want denied work to remain blocked", deniedDispatch)
	}

	runsOutput := run("runs", "--json")
	if !strings.Contains(runsOutput, `"status": "completed"`) || strings.Count(runsOutput, `"run_id":`) != 1 {
		t.Fatalf("runs output = %s, want only approved work run completed", runsOutput)
	}
	logsOutput := run("logs", "--json")
	for _, want := range []string{
		`"type": "approval.resolved"`,
		`"status": "approved"`,
		`"status": "denied"`,
		`"type": "run.finished"`,
	} {
		if !strings.Contains(logsOutput, want) {
			t.Fatalf("logs output = %s, want %s", logsOutput, want)
		}
	}
	statusOutput := run("work", "status")
	for _, want := range []string{"work_items=2", "open_work_items=1", "active_run_attempts=0", "pending_approvals=0"} {
		if !strings.Contains(statusOutput, want) {
			t.Fatalf("work status output = %s, want %s", statusOutput, want)
		}
	}
}

func TestRunApprovalsSupportFiltersAreReadOnly(t *testing.T) {
	t.Parallel()

	root := createRuntimeRoot(t)
	fixture := seedApprovalSupportFilterRuntime(t, root)

	var supportedOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"approvals", "supported", "--json"}, strings.NewReader(""), &supportedOutput); err != nil {
		t.Fatalf("Run(approvals supported --json) error = %v", err)
	}

	var supportedPayload struct {
		Approvals []struct {
			ApprovalID      int64  `json:"approval_id"`
			TaskKey         string `json:"task_key"`
			Status          string `json:"status"`
			ResolverSupport string `json:"resolver_support"`
			RunID           *int64 `json:"run_id,omitempty"`
		} `json:"approvals"`
	}
	if err := json.Unmarshal(supportedOutput.Bytes(), &supportedPayload); err != nil {
		t.Fatalf("supported approvals json = %v\n%s", err, supportedOutput.String())
	}
	if len(supportedPayload.Approvals) != 1 {
		t.Fatalf("supported approvals len = %d, want 1\n%s", len(supportedPayload.Approvals), supportedOutput.String())
	}
	if supportedPayload.Approvals[0].ApprovalID != fixture.SupportedApprovalID {
		t.Fatalf("supported approval id = %d, want %d", supportedPayload.Approvals[0].ApprovalID, fixture.SupportedApprovalID)
	}
	if supportedPayload.Approvals[0].TaskKey != "supported-approval-review" {
		t.Fatalf("supported task key = %q, want supported-approval-review", supportedPayload.Approvals[0].TaskKey)
	}
	if supportedPayload.Approvals[0].ResolverSupport != "supported" {
		t.Fatalf("supported resolver = %q, want supported", supportedPayload.Approvals[0].ResolverSupport)
	}
	if supportedPayload.Approvals[0].RunID == nil || *supportedPayload.Approvals[0].RunID != fixture.SupportedRunID {
		t.Fatalf("supported run id = %v, want %d", supportedPayload.Approvals[0].RunID, fixture.SupportedRunID)
	}

	var unsupportedOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"approvals", "unsupported"}, strings.NewReader(""), &unsupportedOutput); err != nil {
		t.Fatalf("Run(approvals unsupported) error = %v", err)
	}
	unsupportedText := unsupportedOutput.String()
	for _, want := range []string{
		fmt.Sprintf("approval=%d", fixture.UnsupportedApprovalID),
		"task=unsupported-approval-review",
		fmt.Sprintf("run=%d", fixture.UnsupportedRunID),
		"status=pending",
		"resolver=unsupported",
	} {
		if !strings.Contains(unsupportedText, want) {
			t.Fatalf("unsupported output = %q, want %q", unsupportedText, want)
		}
	}
	if strings.Contains(unsupportedText, "task=supported-approval-review") || strings.Contains(unsupportedText, " resolver=supported\n") {
		t.Fatalf("unsupported output = %q, should not include supported approval", unsupportedText)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	for _, approvalID := range []int64{fixture.SupportedApprovalID, fixture.UnsupportedApprovalID} {
		approval, err := store.GetApproval(context.Background(), approvalID)
		if err != nil {
			t.Fatalf("GetApproval(%d) error = %v", approvalID, err)
		}
		if approval.Status != "pending" {
			t.Fatalf("approval %d status = %q, want pending after list filters", approvalID, approval.Status)
		}
	}
}

func TestRunTransitionSetUsesSelectedProject(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}

	var stdout bytes.Buffer
	err := Run(
		context.Background(),
		root,
		[]string{"transition", "set", "cutover", "confirm", "because", "cli smoke"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "project="+testProjectKey) || !strings.Contains(output, "state=cutover") {
		t.Fatalf("stdout = %q, want transition status for alpha cutover", output)
	}
}

func TestRunTaskCreateJSON(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(
		context.Background(),
		root,
		[]string{"task", "create", "--project", testProjectKey, "--title", "cutover smoke"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload struct {
		ID     int64  `json:"id"`
		Key    string `json:"key"`
		Status string `json:"status"`
		Scope  string `json:"scope"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("task create json = %v", err)
	}
	if payload.Status != "queued" {
		t.Fatalf("Status = %q, want queued", payload.Status)
	}
	if payload.Scope != "project" {
		t.Fatalf("Scope = %q, want project", payload.Scope)
	}
	if payload.ID == 0 || payload.Key == "" {
		t.Fatalf("payload = %+v, want populated task identity", payload)
	}
}

func TestRunTaskRunJSON(t *testing.T) {
	configureLifecycleHarnessDriver(t)
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)
	cleanupTaskRunWorktree(t, testProjectKey)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(
		context.Background(),
		root,
		[]string{"transition", "set", "cutover", "confirm", "because", "allow cli run"},
		strings.NewReader(""),
		&bytes.Buffer{},
	); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	var stdout bytes.Buffer
	err := Run(
		context.Background(),
		root,
		[]string{"task", "run", "--project", testProjectKey, "--title", "run from cli", "--json"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var payload struct {
		Task struct {
			Status string `json:"status"`
		} `json:"task"`
		Run struct {
			Status string `json:"status"`
		} `json:"run"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("task run json = %v", err)
	}
	if payload.Task.Status != "completed" {
		t.Fatalf("Task.Status = %q, want completed", payload.Task.Status)
	}
	if payload.Run.Status != "completed" {
		t.Fatalf("Run.Status = %q, want completed", payload.Run.Status)
	}
}

func TestRunSkillsCrudAndInvokeJSON(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "skills", "echo-skill.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(script dir) error = %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"echo complete","output":{"message":"hello"}}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	createSpecPath := filepath.Join(root, "echo-skill.json")
	if err := os.WriteFile(createSpecPath, []byte(`{
  "key": "echo-skill",
  "title": "Echo Skill",
  "summary": "Echoes a structured response.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/echo-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Echo input.",
    "When to Use": "When testing.",
    "Inputs": "A message.",
    "Procedure": "Read and echo.",
    "Outputs": "A JSON response.",
    "Constraints": "Stay deterministic.",
    "Success Criteria": "The caller gets a stable response."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(spec) error = %v", err)
	}

	updateSpecPath := filepath.Join(root, "echo-skill-v2.json")
	if err := os.WriteFile(updateSpecPath, []byte(`{
  "key": "echo-skill",
  "title": "Echo Skill",
  "summary": "Updated summary.",
  "status": "active",
  "version": "1.0.1",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/echo-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Echo input.",
    "When to Use": "When testing.",
    "Inputs": "A message.",
    "Procedure": "Read and echo.",
    "Outputs": "A JSON response.",
    "Constraints": "Stay deterministic.",
    "Success Criteria": "The caller gets a stable response."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(update spec) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"skills", "create", "--spec", createSpecPath, "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills create) error = %v", err)
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "list", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills list) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "echo-skill") {
		t.Fatalf("skills list output = %q, want echo-skill", stdout.String())
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "get", "echo-skill", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills get) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"version\": \"1.0.0\"") {
		t.Fatalf("skills get output = %q, want version 1.0.0", stdout.String())
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "invoke", "echo-skill", "--input", `{"message":"hello"}`, "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills invoke) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "echo complete") {
		t.Fatalf("skills invoke output = %q, want echo summary", stdout.String())
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "update", "echo-skill", "--spec", updateSpecPath, "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills update) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"version\": \"1.0.1\"") {
		t.Fatalf("skills update output = %q, want version 1.0.1", stdout.String())
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "delete", "echo-skill", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills delete) error = %v", err)
	}

	stdout.Reset()
	if err := Run(context.Background(), root, []string{"skills", "list", "--json"}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(skills list after delete) error = %v", err)
	}
	if strings.Contains(stdout.String(), "echo-skill") {
		t.Fatalf("skills list after delete output = %q, should not contain echo-skill", stdout.String())
	}
}

func TestRunSkillsInvokeLifecycleVisibleInLogsAndOverview(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "skills", "audit-skill.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(script dir) error = %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"audit complete","output":{"message":"tracked"}}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	specPath := filepath.Join(root, "audit-skill.json")
	if err := os.WriteFile(specPath, []byte(`{
  "key": "audit-skill",
  "title": "Audit Skill",
  "summary": "Returns a tracked response.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/audit-skill.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Echo input.",
    "When to Use": "When testing.",
    "Inputs": "A message.",
    "Procedure": "Read and echo.",
    "Outputs": "A JSON response.",
    "Constraints": "Stay deterministic.",
    "Success Criteria": "The caller gets a stable response."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(spec) error = %v", err)
	}

	if err := Run(context.Background(), root, []string{"skills", "create", "--spec", specPath, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(skills create) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"skills", "invoke", "audit-skill", "--input", `{"message":"hello"}`, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(skills invoke) error = %v", err)
	}
	err := Run(context.Background(), root, []string{"skills", "invoke", "missing-skill", "--input", `{"message":"fail"}`, "--json"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil {
		t.Fatal("Run(skills invoke missing-skill) error = nil, want failure")
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "skill.lifecycle_recorded"`,
		`"type": "skill.artifact_recorded"`,
		`"skill_key": "audit-skill"`,
		`"handler_ref": "scripts/skills/audit-skill.sh"`,
		`"execution_profile": "restricted_command_v1"`,
		`"permissions": [`,
		`"runtime_effect": "durable_reviewable_artifact"`,
		`"artifact_type": "skill_output"`,
		`"status": "review_required"`,
		`"skill_key": "missing-skill"`,
		`"outcome": "failure"`,
		`"error_code": "not_found"`,
		`"runtime_effect": "not_invoked"`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}

	var overviewOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"overview", "--json"}, strings.NewReader(""), &overviewOutput); err != nil {
		t.Fatalf("Run(overview --json) error = %v", err)
	}
	for _, want := range []string{
		`"skill_activity"`,
		`"invoke_success_count": 1`,
		`"invoke_failure_count": 1`,
		`"durable_reviewable_artifact_count": 1`,
		`"review_required_artifact_count": 1`,
		`"delegation_truth"`,
		`"runtime_status": "not_proven"`,
		`"companion_work_path": "governed_work_items"`,
	} {
		if !strings.Contains(overviewOutput.String(), want) {
			t.Fatalf("overview output = %s, want %s", overviewOutput.String(), want)
		}
	}

	var artifactsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"skills", "artifacts", "--json"}, strings.NewReader(""), &artifactsOutput); err != nil {
		t.Fatalf("Run(skills artifacts --json) error = %v", err)
	}
	for _, want := range []string{
		`"skill_key": "audit-skill"`,
		`"artifact_type": "skill_output"`,
		`"status": "review_required"`,
		`"summary": "audit complete"`,
		`"output_json": "{\"message\":\"tracked\"}"`,
	} {
		if !strings.Contains(artifactsOutput.String(), want) {
			t.Fatalf("skills artifacts output = %s, want %s", artifactsOutput.String(), want)
		}
	}

	var artifactShowOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"skills", "artifact", "show", "1", "--json"}, strings.NewReader(""), &artifactShowOutput); err != nil {
		t.Fatalf("Run(skills artifact show) error = %v", err)
	}
	if !strings.Contains(artifactShowOutput.String(), `"skill_key": "audit-skill"`) || !strings.Contains(artifactShowOutput.String(), `"status": "review_required"`) {
		t.Fatalf("skills artifact show output = %s, want reviewable audit artifact", artifactShowOutput.String())
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	if strings.Contains(jobsOutput.String(), `"jobs": [`) && strings.Contains(jobsOutput.String(), `"id"`) {
		t.Fatalf("jobs output = %s, skill artifact must not create jobs", jobsOutput.String())
	}
}

func TestRunSkillsArtifactReviewAcceptPromotesQueuedWork(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	seedReviewableSkill(t, root, "review-plan", "review plan ready", `{"title":"Prepare operator rollout","next_step":"queue"}`)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}

	var invokeOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"skills", "invoke", "review-plan", "--json"}, strings.NewReader(""), &invokeOutput); err != nil {
		t.Fatalf("Run(skills invoke) error = %v", err)
	}
	if !strings.Contains(invokeOutput.String(), `"status": "review_required"`) || !strings.Contains(invokeOutput.String(), `"runtime_effect": "durable_reviewable_artifact"`) {
		t.Fatalf("skills invoke output = %s, want review-required artifact", invokeOutput.String())
	}

	var acceptOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"skills", "artifact", "review", "accept", "1", "--json"}, strings.NewReader(""), &acceptOutput); err != nil {
		t.Fatalf("Run(skills artifact review accept) error = %v", err)
	}
	for _, want := range []string{
		`"decision": "accepted"`,
		`"status": "accepted"`,
		`"work_created": true`,
		`"key": "skill-artifact-1"`,
		`"requested_by": "skill_artifact_review:1"`,
	} {
		if !strings.Contains(acceptOutput.String(), want) {
			t.Fatalf("accept output = %s, want %s", acceptOutput.String(), want)
		}
	}

	var repeatOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"skills", "artifact", "review", "accept", "1", "--json"}, strings.NewReader(""), &repeatOutput); err != nil {
		t.Fatalf("Run(skills artifact review accept repeat) error = %v", err)
	}
	if !strings.Contains(repeatOutput.String(), `"decision": "accepted"`) || !strings.Contains(repeatOutput.String(), `"work_created": false`) || !strings.Contains(repeatOutput.String(), `"key": "skill-artifact-1"`) {
		t.Fatalf("repeat accept output = %s, want idempotent linked work", repeatOutput.String())
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	if strings.Count(jobsOutput.String(), `"task_key": "skill-artifact-1"`) != 1 {
		t.Fatalf("jobs output = %s, want one promoted skill artifact task", jobsOutput.String())
	}

	var logsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"logs", "--json"}, strings.NewReader(""), &logsOutput); err != nil {
		t.Fatalf("Run(logs --json) error = %v", err)
	}
	for _, want := range []string{
		`"type": "skill.artifact_reviewed"`,
		`"decision": "accepted"`,
		`"follow_on_task_key": "skill-artifact-1"`,
		`"repeated": true`,
	} {
		if !strings.Contains(logsOutput.String(), want) {
			t.Fatalf("logs output = %s, want %s", logsOutput.String(), want)
		}
	}

	var overviewOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"overview", "--json"}, strings.NewReader(""), &overviewOutput); err != nil {
		t.Fatalf("Run(overview --json) error = %v", err)
	}
	for _, want := range []string{
		`"review_required_artifact_count": 0`,
		`"accepted_artifact_count": 1`,
		`"rejected_artifact_count": 0`,
		`"archived_artifact_count": 0`,
	} {
		if !strings.Contains(overviewOutput.String(), want) {
			t.Fatalf("overview output = %s, want %s", overviewOutput.String(), want)
		}
	}
}

func TestRunSkillsArtifactReviewRejectAndArchiveDoNotCreateWork(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	seedReviewableSkill(t, root, "review-note", "review note ready", `{"title":"Draft note","next_step":"inspect"}`)
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	for index := 0; index < 2; index++ {
		if err := Run(context.Background(), root, []string{"skills", "invoke", "review-note", "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("Run(skills invoke %d) error = %v", index, err)
		}
	}

	var rejectOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"skills", "artifact", "review", "reject", "1", "--json"}, strings.NewReader(""), &rejectOutput); err != nil {
		t.Fatalf("Run(skills artifact review reject) error = %v", err)
	}
	if !strings.Contains(rejectOutput.String(), `"decision": "rejected"`) || !strings.Contains(rejectOutput.String(), `"work_created": false`) {
		t.Fatalf("reject output = %s, want rejected with no work", rejectOutput.String())
	}
	var repeatRejectOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"skills", "artifact", "review", "reject", "1", "--json"}, strings.NewReader(""), &repeatRejectOutput); err != nil {
		t.Fatalf("Run(skills artifact review reject repeat) error = %v", err)
	}
	if !strings.Contains(repeatRejectOutput.String(), `"repeated": true`) || !strings.Contains(repeatRejectOutput.String(), `"work_created": false`) {
		t.Fatalf("repeat reject output = %s, want safe repeated rejection", repeatRejectOutput.String())
	}

	var archiveOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"skills", "artifact", "review", "archive", "2", "--json"}, strings.NewReader(""), &archiveOutput); err != nil {
		t.Fatalf("Run(skills artifact review archive) error = %v", err)
	}
	if !strings.Contains(archiveOutput.String(), `"decision": "archived"`) || !strings.Contains(archiveOutput.String(), `"work_created": false`) {
		t.Fatalf("archive output = %s, want archived with no work", archiveOutput.String())
	}

	var jobsOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"jobs", "--json"}, strings.NewReader(""), &jobsOutput); err != nil {
		t.Fatalf("Run(jobs --json) error = %v", err)
	}
	if strings.Contains(jobsOutput.String(), `"task_key": "skill-artifact-`) {
		t.Fatalf("jobs output = %s, reject/archive must not create work", jobsOutput.String())
	}

	var overviewOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"overview", "--json"}, strings.NewReader(""), &overviewOutput); err != nil {
		t.Fatalf("Run(overview --json) error = %v", err)
	}
	for _, want := range []string{
		`"review_required_artifact_count": 0`,
		`"accepted_artifact_count": 0`,
		`"rejected_artifact_count": 1`,
		`"archived_artifact_count": 1`,
	} {
		if !strings.Contains(overviewOutput.String(), want) {
			t.Fatalf("overview output = %s, want %s", overviewOutput.String(), want)
		}
	}
}

func TestRunSkillsInvokeUsesSelectedProjectTransitionState(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "skills", "audit-note.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(script dir) error = %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"status":"ok","summary":"audit note recorded"}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}

	createSpecPath := filepath.Join(root, "audit-note.json")
	if err := os.WriteFile(createSpecPath, []byte(`{
  "key": "audit-note",
  "title": "Audit Note",
  "summary": "Writes a limited action note.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.mutate.isolated:docs_audit_note"],
  "handler_type": "command",
  "handler_ref": "scripts/skills/audit-note.sh",
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Write an audit note.",
    "When to Use": "When testing transition policy.",
    "Inputs": "A structured note.",
    "Procedure": "Record the note.",
    "Outputs": "A JSON response.",
    "Constraints": "Stay deterministic.",
    "Success Criteria": "The caller gets a stable response."
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(spec) error = %v", err)
	}

	if err := Run(context.Background(), root, []string{"skills", "create", "--spec", createSpecPath, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(skills create) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"project", "select", testProjectKey}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(project select) error = %v", err)
	}
	if err := Run(
		context.Background(),
		root,
		[]string{"transition", "set", "limited_action", "allow=docs_audit_note", "confirm", "because", "cli transition smoke"},
		strings.NewReader(""),
		&bytes.Buffer{},
	); err != nil {
		t.Fatalf("Run(transition set) error = %v", err)
	}

	var stdout bytes.Buffer
	err := Run(
		context.Background(),
		root,
		[]string{"skills", "invoke", "audit-note", "--json"},
		strings.NewReader(""),
		&stdout,
	)
	if err != nil {
		t.Fatalf("Run(skills invoke) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "audit note recorded") {
		t.Fatalf("skills invoke output = %q, want audit note summary", stdout.String())
	}
}

func TestNewJobServiceIncludesSupervisor(t *testing.T) {
	t.Parallel()

	service := newJobService(bootstrap.App{})
	if service.Supervisor == nil {
		t.Fatal("newJobService() Supervisor = nil, want concrete supervisor")
	}
	if _, ok := service.Supervisor.(supervision.Service); !ok {
		t.Fatalf("newJobService() Supervisor = %T, want supervision.Service", service.Supervisor)
	}
}

func TestInvokeServedProjectStatusFallsBackToScopeAndMode(t *testing.T) {
	t.Parallel()

	response, err := invokeServedProjectStatus(context.Background(), bootstrap.App{}, capabilities.InvokeRequest{
		Scope: capabilities.ScopeRef{
			Kind: "global",
		},
		Execution: capabilities.ExecutionRequest{
			Mode: "local",
		},
	})
	if err != nil {
		t.Fatalf("invokeServedProjectStatus() error = %v", err)
	}
	if string(response.Output) != "scope=global mode=local\n" {
		t.Fatalf("response output = %q, want %q", response.Output, "scope=global mode=local\n")
	}
}

func TestInvokeServedProjectStatusFallsBackToProjectScopeLabel(t *testing.T) {
	t.Parallel()

	response, err := invokeServedProjectStatus(context.Background(), bootstrap.App{}, capabilities.InvokeRequest{
		Scope: capabilities.ScopeRef{
			Kind:       "project",
			ProjectKey: "alpha",
		},
		Execution: capabilities.ExecutionRequest{
			Mode: "local",
		},
	})
	if err != nil {
		t.Fatalf("invokeServedProjectStatus() error = %v", err)
	}
	if string(response.Output) != "scope=alpha mode=local\n" {
		t.Fatalf("response output = %q, want %q", response.Output, "scope=alpha mode=local\n")
	}
}

func testRepoRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	mustMkdirAll := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	mustWriteFile := func(path string, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	mustMkdirAll(filepath.Join(root, "config"))
	mustMkdirAll(filepath.Join(root, "data"))
	mustMkdirAll(filepath.Join(root, "registry"))
	mustMkdirAll(filepath.Join(root, "state", "cache"))
	mustMkdirAll(filepath.Join(root, "alpha"))

	mustWriteFile(filepath.Join(root, "config", "projects.yaml"), `
version: 1
projects:
  - key: alpha-cli
    name: Alpha
    project_class: github_backed_project
    git_root: ../alpha
    default_branch: main
    github:
      repo: acme/alpha
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ..
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`)
	mustWriteFile(filepath.Join(root, "config", "executors.yaml"), `
version: 1
executors:
  - key: codex_headless
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
routes:
  - name: default
    match:
      task_kinds: [general, plan, build, review, qa, research]
      scopes: [global, odin-core, project, new-project]
    preferred: [codex_headless]
`)
	mustWriteFile(filepath.Join(root, "config", "odin.yaml"), `
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`)

	mustWriteFile(filepath.Join(root, "README.md"), "alpha test repo\n")
	mustWriteFile(filepath.Join(root, "alpha", "README.md"), "alpha nested repo\n")
	runGitIn := func(dir string, args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test User",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test User",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}

	runGitIn(root, "init", "-b", "main")
	runGitIn(root, "add", ".")
	runGitIn(root, "commit", "-m", "test fixture")

	runGitIn(filepath.Join(root, "alpha"), "init", "-b", "main")
	runGitIn(filepath.Join(root, "alpha"), "add", ".")
	runGitIn(filepath.Join(root, "alpha"), "commit", "-m", "alpha fixture")

	return root
}

func seedPendingApprovalRuntime(t *testing.T, root string) (int64, int64, int64) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(root, "repos", "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "finance-transfer-review",
		Title:       "Prepare Robinhood transfer review",
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "blocked",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	return approval.ID, task.ID, run.ID
}

type approvalSupportFilterFixture struct {
	SupportedApprovalID   int64
	SupportedRunID        int64
	UnsupportedApprovalID int64
	UnsupportedRunID      int64
}

func seedApprovalSupportFilterRuntime(t *testing.T, root string) approvalSupportFilterFixture {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	unsupportedApproval, unsupportedRun := seedApprovalSupportFilterRecord(t, ctx, store, "approval-unsupported", "unsupported-approval-review")
	supportedApproval, supportedRun := seedApprovalSupportFilterRecord(t, ctx, store, "approval-supported", "supported-approval-review")

	if _, err := (checkpoints.Service{Store: store}).Compact(ctx, checkpoints.CompactParams{
		TaskID:            supportedApproval.TaskID,
		RunID:             &supportedRun.ID,
		Trigger:           checkpoints.TriggerApprovalWait,
		CheckpointKey:     "supported-approval-review",
		Objective:         "Supported approval review",
		TaskStatus:        "blocked",
		BlockingReason:    "approval_required",
		LastCompletedStep: "review prepared",
		ApprovalSummary:   "approval pending",
		ToolResults: []checkpoints.ToolResult{
			{
				Key:     "robinhood_transfer_prepare",
				Summary: "review prepared",
				Facts: map[string]string{
					"session_state": "review_ready",
				},
			},
		},
	}); err != nil {
		t.Fatalf("Compact(supported approval wait) error = %v", err)
	}

	return approvalSupportFilterFixture{
		SupportedApprovalID:   supportedApproval.ID,
		SupportedRunID:        supportedRun.ID,
		UnsupportedApprovalID: unsupportedApproval.ID,
		UnsupportedRunID:      unsupportedRun.ID,
	}
}

func seedApprovalSupportFilterRecord(t *testing.T, ctx context.Context, store *sqlite.Store, projectKey string, taskKey string) (sqlite.Approval, sqlite.Run) {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           projectKey,
		Name:          projectKey,
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), projectKey),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(%s) error = %v", projectKey, err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         taskKey,
		Title:       taskKey,
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(%s) error = %v", taskKey, err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "blocked",
	})
	if err != nil {
		t.Fatalf("StartRun(%s) error = %v", taskKey, err)
	}
	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval(%s) error = %v", taskKey, err)
	}
	return approval, run
}

func listRuntimeTaskRunIDs(t *testing.T, root string, taskID int64) []int64 {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	rows, err := store.DB().QueryContext(context.Background(), `SELECT id FROM runs WHERE task_id = ? ORDER BY id ASC`, taskID)
	if err != nil {
		t.Fatalf("QueryContext(runs) error = %v", err)
	}
	defer rows.Close()

	var runIDs []int64
	for rows.Next() {
		var runID int64
		if err := rows.Scan(&runID); err != nil {
			t.Fatalf("rows.Scan() error = %v", err)
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() error = %v", err)
	}

	return runIDs
}

func cleanupTaskRunWorktree(t *testing.T, projectKey string) {
	t.Helper()

	path := worktrees.ResolvePath(worktrees.PathParams{
		Root:       worktrees.DefaultRoot(),
		ProjectKey: projectKey,
		TaskID:     1,
		RunID:      1,
		Try:        1,
	})
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("RemoveAll(%s) error = %v", path, err)
	}
}

func TestRunRunsShowRendersFailureAnalysis(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(root, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha-cli",
		Name:          "Alpha CLI",
		Scope:         "project",
		GitRoot:       root,
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "failure-task",
		Title:       "Failure task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	const artifactJSON = `{"failure_analysis":{"category":"test_failure","suggested_fix":"Inspect failing test output and repair the regression.","next_step_target":"test","retry_recommended":true,"follow_up":{"recommended":true,"title":"Fix flaky test","reason":"needs a focused repair"}}}`
	if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:          run.ID,
		Status:         "failed",
		Summary:        "test failed",
		TerminalReason: "failed",
		ArtifactsJSON:  artifactJSON,
	}); err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Run(context.Background(), root, []string{"runs", "show", strconv.FormatInt(run.ID, 10)}, strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Run(runs show) error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"run=1",
		"task=failure-task",
		"status=failed",
		"artifacts_json=" + artifactJSON,
		"failure_analysis_category=test_failure",
		"failure_analysis_suggested_fix=Inspect failing test output and repair the regression.",
		"failure_analysis_next_step_target=test",
		"failure_analysis_retry_recommended=true",
		"failure_analysis_follow_up_recommended=true",
		"failure_analysis_follow_up_title=Fix flaky test",
		"failure_analysis_follow_up_reason=needs a focused repair",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("runs show output = %q, want substring %q", output, want)
		}
	}
}

func seedStatusCompanionSwarms(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "swarm-project",
		Name:          "Swarm Project",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             string(initiatives.KindManagedProject),
		Status:           "active",
		Summary:          "Swarm initiative",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	activeTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "status-swarm-active",
		Title:        "Active swarm",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(active) error = %v", err)
	}
	activeDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    activeTask.ID,
		ProjectID:       project.ID,
		Scope:           activeTask.Scope,
		DelegationKey:   "status-swarm-active-child",
		Role:            "builder",
		ActionClass:     "mutation",
		ActionKey:       "implement",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"active","swarm":{"requested_budget":2,"max_children":2}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(active) error = %v", err)
	}
	activeChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "status-swarm-active-child",
		Title:       "Active child",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(active child) error = %v", err)
	}
	activeRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     activeChild.ID,
		Executor:   "codex",
		Attempt:    1,
		Status:     "running",
		TaskStatus: "running",
	})
	if err != nil {
		t.Fatalf("StartRun(active child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: activeDelegation.ID,
		ChildTaskID:  activeChild.ID,
		ChildRunID:   &activeRun.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(active) error = %v", err)
	}
	if _, err := store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
		DelegationID: activeDelegation.ID,
		ArtifactType: "result",
		Summary:      "Active child completed",
		DetailsJSON:  `{"status":"completed","confidence":0.9,"evidence_refs":["status/active"],"unresolved_risks":[],"proposed_next_actions":[],"proposed_memory_candidates":[]}`,
	}); err != nil {
		t.Fatalf("CreateDelegationArtifact(active) error = %v", err)
	}

	approvalTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "status-swarm-approval",
		Title:        "Approval swarm",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval) error = %v", err)
	}
	approvalDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    approvalTask.ID,
		ProjectID:       project.ID,
		Scope:           approvalTask.Scope,
		DelegationKey:   "status-swarm-approval-child",
		Role:            "builder",
		ActionClass:     "mutation",
		ActionKey:       "implement",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "review_gate",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"approval","swarm":{"requested_budget":1,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(approval) error = %v", err)
	}
	approvalChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "status-swarm-approval-child",
		Title:       "Approval child",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval child) error = %v", err)
	}
	if _, _, err := store.BlockTaskAndRequestApproval(ctx, sqlite.BlockTaskAndRequestApprovalParams{
		TaskID:      approvalChild.ID,
		RunID:       nil,
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("BlockTaskAndRequestApproval(approval child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: approvalDelegation.ID,
		ChildTaskID:  approvalChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(approval) error = %v", err)
	}

	budgetTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "status-swarm-budget",
		Title:        "Budget swarm",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(budget) error = %v", err)
	}
	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: budgetTask.ID,
		Reason: "budget_exhausted",
	}); err != nil {
		t.Fatalf("BlockTask(budget) error = %v", err)
	}
	budgetDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    budgetTask.ID,
		ProjectID:       project.ID,
		Scope:           budgetTask.Scope,
		DelegationKey:   "status-swarm-budget-child",
		Role:            "reviewer",
		ActionClass:     "analysis",
		ActionKey:       "review",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge",
		ArtifactTarget:  "report",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"budget","swarm":{"requested_budget":3,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(budget) error = %v", err)
	}
	budgetChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "status-swarm-budget-child",
		Title:       "Budget child",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(budget child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: budgetDelegation.ID,
		ChildTaskID:  budgetChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(budget) error = %v", err)
	}
}

func configureLifecycleHarnessDriver(t *testing.T) {
	configureLifecycleHarnessDriverStatus(t, "completed", "driver test ok")
}

func configureLifecycleHarnessDriverStatus(t *testing.T, status string, output string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "codex-driver.sh")
	script := fmt.Sprintf(`#!/usr/bin/env bash
payload="$(cat)"
PAYLOAD="$payload" python3 - <<'PY'
import json
import os

request = json.loads(os.environ["PAYLOAD"])
action = request.get("action")
if action == "health":
    print(json.dumps({"status":"healthy","details":"lifecycle test driver healthy"}))
else:
    print(json.dumps({"status":%q,"output":%q,"handle":{"external_id":"fixture-driver"}}))
PY
`, status, output)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_CODEX_DRIVER", path)
}

func configureLifecycleMetadataEchoDriver(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "codex-driver.sh")
	script := `#!/usr/bin/env bash
payload="$(cat)"
PAYLOAD="$payload" python3 - <<'PY'
import json
import os

request = json.loads(os.environ["PAYLOAD"])
action = request.get("action")
if action == "health":
    print(json.dumps({"status":"healthy","details":"lifecycle metadata echo driver healthy"}))
else:
    metadata = (request.get("task") or {}).get("metadata") or {}
    output = "|".join([
        metadata.get("repo_root", ""),
        metadata.get("worktree_path", ""),
        metadata.get("branch_name", ""),
    ])
    print(json.dumps({"status":"completed","output":output,"handle":{"external_id":"metadata-echo-driver"}}))
PY
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_CODEX_DRIVER", path)
}

func installRepoCodexDriverScript(t *testing.T, root string) {
	t.Helper()

	sourcePath := filepath.Join("..", "..", "..", "scripts", "drivers", "codex-headless.sh")
	contents, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", sourcePath, err)
	}
	targetPath := filepath.Join(root, "scripts", "drivers", "codex-headless.sh")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(driver dir) error = %v", err)
	}
	if err := os.WriteFile(targetPath, contents, 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", targetPath, err)
	}
}

func seedDelegationSkillFixture(t *testing.T, root string) {
	t.Helper()

	skillDir := filepath.Join(root, "registry", "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(skill dir) error = %v", err)
	}
	stubPath := filepath.Join(root, "scripts", "skills", "registry-skill-stub.sh")
	if err := os.MkdirAll(filepath.Dir(stubPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(skill script dir) error = %v", err)
	}
	if err := os.WriteFile(stubPath, []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' '{"result":"ok"}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(skill stub) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "pixel-perfect-ui-ux-designer.md"), []byte(`---
kind: skill
key: pixel-perfect-ui-ux-designer
title: Pixel Perfect UI/UX Designer
summary: Test delegation skill.
status: active
version: "1.0.0"
enabled: true
tags: [design]
owners: [odin-core]
strictness: adaptive
applies_to: [design]
scopes: [project]
permissions: [repo.read]
handler_type: command
handler_ref: scripts/skills/registry-skill-stub.sh
timeout_seconds: 15
input_schema:
  type: object
output_schema:
  type: object
---

# Pixel Perfect UI/UX Designer

## Purpose
Test skill.

## When to Use
Delegation tests.

## Inputs
Prompt.

## Procedure
Read and respond.

## Outputs
Structured result.

## Constraints
No mutation.

## Success Criteria
Prompt includes skill content.
`), 0o644); err != nil {
		t.Fatalf("WriteFile(skill fixture) error = %v", err)
	}
}

func seedReviewableSkill(t *testing.T, root string, key string, summary string, outputJSON string) {
	t.Helper()

	scriptPath := filepath.Join(root, "scripts", "skills", key+".sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(skill script dir) error = %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte(fmt.Sprintf(`#!/usr/bin/env bash
cat >/dev/null
printf '%%s\n' '{"status":"ok","summary":%q,"output":%s}'
`, summary, outputJSON)), 0o755); err != nil {
		t.Fatalf("WriteFile(skill script) error = %v", err)
	}

	specPath := filepath.Join(root, key+".json")
	spec := fmt.Sprintf(`{
  "key": %q,
  "title": "Reviewable Skill",
  "summary": "Produces a reviewable artifact.",
  "status": "active",
  "version": "1.0.0",
  "enabled": true,
  "tags": ["testing"],
  "owners": ["odin-core"],
  "strictness": "rigid",
  "applies_to": ["testing"],
  "scopes": ["project"],
  "permissions": ["repo.read"],
  "handler_type": "command",
  "handler_ref": %q,
  "timeout_seconds": 15,
  "input_schema": {"type":"object"},
  "output_schema": {"type":"object"},
  "sections": {
    "Purpose": "Return a deterministic review artifact.",
    "When to Use": "When testing skill artifact review.",
    "Inputs": "None.",
    "Procedure": "Return JSON.",
    "Outputs": "A JSON response.",
    "Constraints": "No mutation.",
    "Success Criteria": "A reviewable artifact is created."
  }
}`, key, filepath.ToSlash(filepath.Join("scripts", "skills", key+".sh")))
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("WriteFile(skill spec) error = %v", err)
	}
	if err := Run(context.Background(), root, []string{"skills", "create", "--spec", specPath, "--json"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("Run(skills create) error = %v", err)
	}
}
