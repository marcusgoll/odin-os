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
	"strings"
	"sync/atomic"
	"testing"

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
	if payload.IntakeInbox.Wiring != clioverview.WiringNotYetWired {
		t.Fatalf("IntakeInbox.Wiring = %q, want %q", payload.IntakeInbox.Wiring, clioverview.WiringNotYetWired)
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
	if strings.Contains(stdout.String(), "scheduler") {
		t.Fatalf("help output = %q, should not claim scheduler command", stdout.String())
	}
}

func TestRunSchedulerCommandIsNotClaimedSurface(t *testing.T) {
	t.Parallel()

	root := testRepoRoot(t)
	var stdout bytes.Buffer

	err := Run(context.Background(), root, []string{"scheduler", "--help"}, strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("Run(scheduler --help) error = nil, want unknown command")
	}
	if got := err.Error(); got != "unknown command: scheduler" {
		t.Fatalf("Run(scheduler --help) error = %q, want unknown command: scheduler", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty output", stdout.String())
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
	t.Helper()

	path := filepath.Join(t.TempDir(), "codex-driver.sh")
	if err := os.WriteFile(path, []byte(`#!/usr/bin/env bash
payload="$(cat)"
PAYLOAD="$payload" python3 - <<'PY'
import json
import os

request = json.loads(os.environ["PAYLOAD"])
action = request.get("action")
if action == "health":
    print(json.dumps({"status":"healthy","details":"lifecycle test driver healthy"}))
else:
    print(json.dumps({"status":"completed","output":"driver test ok","handle":{"external_id":"fixture-driver"}}))
PY
`), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_CODEX_DRIVER", path)
}
