package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestShellRestoresValidSessionOnStartup(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	if err := env.SessionStore.Save(Cache{
		ProjectKey: "alpha",
		Mode:       ModeAct,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if shell.state.Mode != ModeAct {
		t.Fatalf("Mode = %q, want %q", shell.state.Mode, ModeAct)
	}
	if shell.state.Scope.Kind != scope.ScopeProject || shell.state.Scope.ProjectKey != "alpha" {
		t.Fatalf("Scope = %+v, want project alpha", shell.state.Scope)
	}
}

func TestShellDowngradesInvalidSessionOnStartup(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	if err := env.SessionStore.Save(Cache{
		ProjectKey: "missing",
		Mode:       ModeAct,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if shell.state.Mode != ModeAsk {
		t.Fatalf("Mode = %q, want %q", shell.state.Mode, ModeAsk)
	}
	if shell.state.Scope.Kind != scope.ScopeGlobal {
		t.Fatalf("Scope = %+v, want global", shell.state.Scope)
	}
}

func TestAskModeHandlesFreeTextWithoutCreatingTask(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "what scope am i in?", &output); err != nil {
		t.Fatalf("HandleLine() error = %v", err)
	}

	if !strings.Contains(output.String(), "global") {
		t.Fatalf("HandleLine() output = %q, want scope answer", output.String())
	}

	views, err := shell.jobs.List(context.Background(), shell.state.Scope)
	if err != nil {
		t.Fatalf("jobs.List() error = %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("jobs len = %d, want 0", len(views))
	}
}

func TestMatchesEventScopeAllowsControlScopedEventsInProjectViews(t *testing.T) {
	t.Parallel()

	if !matchesEventScope("workspace", scope.Resolution{Kind: scope.ScopeOdinCore, ProjectKey: "odin-core"}) {
		t.Fatalf("matchesEventScope(workspace, odin-core) = false, want true")
	}
	if !matchesEventScope("initiative", scope.Resolution{Kind: scope.ScopeProject, ProjectKey: "alpha"}) {
		t.Fatalf("matchesEventScope(initiative, project) = false, want true")
	}
	if matchesEventScope("workspace", scope.Resolution{Kind: scope.ScopeNewProject}) {
		t.Fatalf("matchesEventScope(workspace, new-project) = true, want false")
	}
}

func TestActModeCreatesTaskInProjectScope(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/mode act", &output); err != nil {
		t.Fatalf("HandleLine(/mode) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "Implement the shell", &output); err != nil {
		t.Fatalf("HandleLine(act input) error = %v", err)
	}

	views, err := shell.jobs.List(context.Background(), scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	})
	if err != nil {
		t.Fatalf("jobs.List() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("jobs len = %d, want 1", len(views))
	}
	if !strings.Contains(output.String(), "created task") {
		t.Fatalf("output = %q, want creation message", output.String())
	}
}

func TestActModeRejectedInGlobalScope(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/mode act", &output); err != nil {
		t.Fatalf("HandleLine() error = %v", err)
	}

	if shell.state.Mode != ModeAsk {
		t.Fatalf("Mode = %q, want ask", shell.state.Mode)
	}
	if !strings.Contains(output.String(), "global scope") {
		t.Fatalf("output = %q, want global-scope rejection", output.String())
	}
}

func TestDoctorCommandRendersStructuredTextOutput(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/doctor", &output); err != nil {
		t.Fatalf("HandleLine(/doctor) error = %v", err)
	}

	for _, want := range []string{"status=", "database=", "registry=", "executor=", "queue=", "projections=", "sources="} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want substring %q", output.String(), want)
		}
	}
}

func TestDoctorCommandSupportsJSONOutput(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	if _, err := env.Store.RecordExecutorHealth(context.Background(), sqlite.RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := env.Store.RecordRegistryVersion(context.Background(), sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "fresh compile",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := env.Store.RecordProjectionFreshness(context.Background(), sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}

	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/doctor json", &output); err != nil {
		t.Fatalf("HandleLine(/doctor json) error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["status"] == nil {
		t.Fatalf("decoded status missing: %#v", decoded)
	}
}

func TestShellHelpIncludesTransitionCommands(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/help", &output); err != nil {
		t.Fatalf("HandleLine(/help) error = %v", err)
	}

	for _, want := range []string{"/transition", "/observe", "/compare"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("help output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellTransitionStatusShowsDefaultInventoryAuthority(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/transition", &output); err != nil {
		t.Fatalf("HandleLine(/transition) error = %v", err)
	}

	for _, want := range []string{
		"project=alpha",
		"state=inventory",
		"controller=legacy_odin",
		"mutation_authority=legacy_odin",
		"odin_can_mutate=false",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("transition output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellTransitionSetShadowRecordsEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/transition set shadow because observe only", &output); err != nil {
		t.Fatalf("HandleLine(/transition set shadow) error = %v", err)
	}

	project, err := env.Store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	transition, err := env.Store.GetProjectTransition(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProjectTransition() error = %v", err)
	}
	if transition.State != string(projects.TransitionStateShadow) {
		t.Fatalf("transition.State = %q, want %q", transition.State, projects.TransitionStateShadow)
	}

	events, err := env.Store.ListEvents(ctx, sqlite.ListEventsParams{ProjectID: &project.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if !hasTransitionEvent(events, runtimeevents.EventProjectTransitionChanged) {
		t.Fatalf("events missing project.transition_changed: %+v", events)
	}
}

func TestShellTransitionSetCutoverRequiresConfirm(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/transition set cutover because take ownership", &output); err != nil {
		t.Fatalf("HandleLine(/transition set cutover) error = %v", err)
	}

	if !strings.Contains(output.String(), "confirm") {
		t.Fatalf("output = %q, want confirm requirement", output.String())
	}
}

func TestShellTransitionSetLimitedActionRequiresAllowlist(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/transition set limited_action confirm because pilot", &output); err != nil {
		t.Fatalf("HandleLine(/transition set limited_action) error = %v", err)
	}

	if !strings.Contains(output.String(), "allow=") {
		t.Fatalf("output = %q, want allowlist requirement", output.String())
	}
}

func TestShellObserveRecordsShadowObservation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/transition set shadow because observe only", &output); err != nil {
		t.Fatalf("HandleLine(/transition set shadow) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/observe legacy deploy observed", &output); err != nil {
		t.Fatalf("HandleLine(/observe) error = %v", err)
	}

	project, err := env.Store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	reports, err := env.Store.ListProjectTransitionReports(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListProjectTransitionReports() error = %v", err)
	}
	if len(reports) != 1 || reports[0].ReportType != "shadow_observation" {
		t.Fatalf("reports = %+v, want one shadow_observation", reports)
	}

	events, err := env.Store.ListEvents(ctx, sqlite.ListEventsParams{ProjectID: &project.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if !hasTransitionEvent(events, runtimeevents.EventProjectShadowObservationRecorded) {
		t.Fatalf("events missing project.shadow_observation_recorded: %+v", events)
	}
}

func TestShellCompareRecordsCompareReport(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/transition set compare because compare live decisions", &output); err != nil {
		t.Fatalf("HandleLine(/transition set compare) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/compare route mismatch on candidate", &output); err != nil {
		t.Fatalf("HandleLine(/compare) error = %v", err)
	}

	project, err := env.Store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	reports, err := env.Store.ListProjectTransitionReports(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListProjectTransitionReports() error = %v", err)
	}
	if len(reports) != 1 || reports[0].ReportType != "compare_report" {
		t.Fatalf("reports = %+v, want one compare_report", reports)
	}

	events, err := env.Store.ListEvents(ctx, sqlite.ListEventsParams{ProjectID: &project.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if !hasTransitionEvent(events, runtimeevents.EventProjectCompareReportRecorded) {
		t.Fatalf("events missing project.compare_report_recorded: %+v", events)
	}
}

func TestShellTransitionRejectedInGlobalScope(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(context.Background(), "/transition", &output); err != nil {
		t.Fatalf("HandleLine(/transition) error = %v", err)
	}

	if !strings.Contains(output.String(), "project scope") {
		t.Fatalf("output = %q, want project-scope rejection", output.String())
	}
}

func newTestEnvironment(t *testing.T) Environment {
	t.Helper()

	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	stateDir := filepath.Join(root, "state", "cache")
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	registry := writeRegistry(t, map[string]string{
		"odin-core": "system_project",
		"alpha":     "github_backed_project",
	})

	store, err := sqlite.Open(filepath.Join(dataDir, "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	return Environment{
		Store:        store,
		Registry:     registry,
		SessionStore: SessionStore{Path: filepath.Join(stateDir, "cli-session.json")},
	}
}

func hasTransitionEvent(events []runtimeevents.Record, want runtimeevents.Type) bool {
	for _, event := range events {
		if event.Type == want {
			return true
		}
	}
	return false
}
