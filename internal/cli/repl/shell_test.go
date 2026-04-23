package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/registry"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
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
	if err := shell.HandleLine(context.Background(), "hello there", &output); err != nil {
		t.Fatalf("HandleLine() error = %v", err)
	}

	if strings.Contains(output.String(), "Phase 05") {
		t.Fatalf("HandleLine() output = %q, want executor-backed answer", output.String())
	}

	views, err := shell.jobs.List(context.Background(), shell.state.Scope)
	if err != nil {
		t.Fatalf("jobs.List() error = %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("jobs len = %d, want 0", len(views))
	}
}

func TestAskModeAnswersScopeQuestionsWithoutCreatingTask(t *testing.T) {
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
	if err := shell.HandleLine(context.Background(), "what scope am i in?", &output); err != nil {
		t.Fatalf("HandleLine(scope question) error = %v", err)
	}

	response := strings.TrimSpace(output.String())
	if strings.HasPrefix(response, "scope=") {
		t.Fatalf("HandleLine() output = %q, want conversational answer", output.String())
	}

	views, err := shell.jobs.List(context.Background(), shell.state.Scope)
	if err != nil {
		t.Fatalf("jobs.List() error = %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("jobs len = %d, want 0", len(views))
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

func TestJobsCancelCancelsQueuedTaskByKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	task, err := env.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha) error = %v", err)
	}

	if err := shell.HandleLine(ctx, "/project alpha", &bytes.Buffer{}); err != nil {
		t.Fatalf("HandleLine(/project alpha) error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/jobs cancel "+task.Key, &output); err != nil {
		t.Fatalf("HandleLine(/jobs cancel) error = %v", err)
	}

	if !strings.Contains(output.String(), "alpha alpha-task cancelled") {
		t.Fatalf("output = %q, want cancelled task summary", output.String())
	}
}

func TestActModeCreatesTaskAndAttemptsRunImmediately(t *testing.T) {
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

	runs, err := shell.runs.List(context.Background(), scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	})
	if err != nil {
		t.Fatalf("runs.List() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs len = %d, want 1 immediate run", len(runs))
	}
	if !strings.Contains(output.String(), "run") {
		t.Fatalf("output = %q, want inline run visibility", output.String())
	}
}

func TestActModePrintsPolicyDenialInlineWhenMutationBlocked(t *testing.T) {
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
	if err := shell.HandleLine(context.Background(), "/transition set shadow because mutation is blocked", &output); err != nil {
		t.Fatalf("HandleLine(/transition set shadow) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/mode act", &output); err != nil {
		t.Fatalf("HandleLine(/mode) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "apply the blocked mutation", &output); err != nil {
		t.Fatalf("HandleLine(act input) error = %v", err)
	}

	if !strings.Contains(strings.ToLower(output.String()), "denied") {
		t.Fatalf("output = %q, want inline policy denial", output.String())
	}

	runs, err := shell.runs.List(context.Background(), scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	})
	if err != nil {
		t.Fatalf("runs.List() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs len = %d, want immediate blocked run record", len(runs))
	}
}

func TestActModePrintsCompletedResultInlineWhenExecutionSucceeds(t *testing.T) {
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
	if err := shell.HandleLine(context.Background(), "/transition set limited_action allow=run_task confirm because shell immediate execution", &output); err != nil {
		t.Fatalf("HandleLine(/transition set limited_action) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "/mode act", &output); err != nil {
		t.Fatalf("HandleLine(/mode) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(context.Background(), "Complete the shell implementation", &output); err != nil {
		t.Fatalf("HandleLine(act input) error = %v", err)
	}

	if !strings.Contains(strings.ToLower(output.String()), "completed") {
		t.Fatalf("output = %q, want inline completed result", output.String())
	}

	runs, err := shell.runs.List(context.Background(), scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	})
	if err != nil {
		t.Fatalf("runs.List() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs len = %d, want completed run record", len(runs))
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

func TestRunsShowUsesActiveRunAndRendersStoredArtifacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	task, err := env.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := env.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	run, err = env.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   run.ID,
		Status:  "completed",
		Summary: "alpha shell summary",
	})
	if err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}
	transcript, err := env.Store.RecordConversationTranscript(ctx, sqlite.RecordConversationTranscriptParams{
		ProjectID:   &project.ID,
		TaskID:      &task.ID,
		RunID:       &run.ID,
		Scope:       "project",
		ScopeKey:    "alpha",
		Mode:        "act",
		Prompt:      "Investigate alpha task",
		Response:    "Alpha transcript response",
		ToolSummary: `{"tools":[]}`,
		Executor:    "codex_headless",
	})
	if err != nil {
		t.Fatalf("RecordConversationTranscript() error = %v", err)
	}
	if _, err := env.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:          &project.ID,
		SourceTranscriptID: &transcript.ID,
		TaskID:             &task.ID,
		RunID:              &run.ID,
		Scope:              "project",
		ScopeKey:           "alpha",
		MemoryType:         "episode",
		Summary:            "Alpha memory summary",
		DetailsJSON:        `{"result":"ok"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	shell.state.ActiveRun = strconv.FormatInt(run.ID, 10)
	output.Reset()
	if err := shell.HandleLine(ctx, "/runs show", &output); err != nil {
		t.Fatalf("HandleLine(/runs show) error = %v", err)
	}

	for _, want := range []string{
		"run=" + strconv.FormatInt(run.ID, 10),
		"task=alpha-task",
		"project=alpha",
		"summary=alpha shell summary",
		"prompt:\nInvestigate alpha task",
		"response:\nAlpha transcript response",
		"memory=",
		"Alpha memory summary",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestRunsShowRejectsRunsOutsideCurrentScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	alphaProject, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	coreProject, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/tmp/odin",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(odin-core) error = %v", err)
	}
	_, _ = alphaProject, coreProject
	coreTask, err := env.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   coreProject.ID,
		Key:         "core-task",
		Title:       "Core task",
		Status:      "running",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(core) error = %v", err)
	}
	coreRun, err := env.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   coreTask.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(core) error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/runs show "+strconv.FormatInt(coreRun.ID, 10), &output); err != nil {
		t.Fatalf("HandleLine(/runs show <id>) error = %v", err)
	}

	if !strings.Contains(output.String(), "unknown run") {
		t.Fatalf("output = %q, want unknown run message", output.String())
	}
}

func TestRunsListIncludesRunIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	task, err := env.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha) error = %v", err)
	}
	run, err := env.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(alpha) error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project alpha) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/runs", &output); err != nil {
		t.Fatalf("HandleLine(/runs) error = %v", err)
	}
	if !strings.Contains(output.String(), "run="+strconv.FormatInt(run.ID, 10)) {
		t.Fatalf("output = %q, want run id in list", output.String())
	}
}

func TestRunsShowActiveFallsBackToLatestRunningRunInScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	task, err := env.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha) error = %v", err)
	}
	run, err := env.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(alpha) error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project alpha) error = %v", err)
	}
	shell.state.ActiveRun = ""
	output.Reset()
	if err := shell.HandleLine(ctx, "/runs show active", &output); err != nil {
		t.Fatalf("HandleLine(/runs show active) error = %v", err)
	}
	if !strings.Contains(output.String(), "run="+strconv.FormatInt(run.ID, 10)) {
		t.Fatalf("output = %q, want fallback active run detail", output.String())
	}
}

func TestRunsCancelMarksActiveRunCancelled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	task, err := env.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha) error = %v", err)
	}
	run, err := env.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(alpha) error = %v", err)
	}

	if err := shell.HandleLine(ctx, "/project alpha", &bytes.Buffer{}); err != nil {
		t.Fatalf("HandleLine(/project alpha) error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/runs cancel "+strconv.FormatInt(run.ID, 10), &output); err != nil {
		t.Fatalf("HandleLine(/runs cancel) error = %v", err)
	}

	for _, want := range []string{
		"run=" + strconv.FormatInt(run.ID, 10),
		"status=cancelled",
		"summary=cancelled by operator",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
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

	for _, want := range []string{"/transition", "/observe", "/compare", "/runs show", "/runs cancel", "show <id>"} {
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

func TestShellProjectAddEnrollsAndSelectsNewProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	projectRoot := filepath.Join(t.TempDir(), "family-ops")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}

	var output bytes.Buffer
	command := "/project add family-ops " + projectRoot + " name=Family-Ops class=local_git_project default_branch=main"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(/project add) error = %v", err)
	}

	if shell.state.Scope.Kind != scope.ScopeProject || shell.state.Scope.ProjectKey != "family-ops" {
		t.Fatalf("Scope = %+v, want project family-ops", shell.state.Scope)
	}

	if _, ok := shell.env.Registry.Lookup("family-ops"); !ok {
		t.Fatalf("expected family-ops in reloaded registry")
	}

	output.Reset()
	if err := shell.HandleLine(ctx, "/project", &output); err != nil {
		t.Fatalf("HandleLine(/project) error = %v", err)
	}
	if !strings.Contains(output.String(), "family-ops") {
		t.Fatalf("project output = %q, want family-ops listed", output.String())
	}

	configPath := registryConfigPath(t, shell.env.Registry)
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(config) error = %v", err)
	}
	if !strings.Contains(string(content), "key: family-ops") {
		t.Fatalf("config = %q, want family-ops manifest entry", string(content))
	}
}

func TestShellProjectAddRejectsNonGitRootWithoutMutatingManifest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	projectRoot := filepath.Join(t.TempDir(), "family-ops")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}

	var output bytes.Buffer
	command := "/project add family-ops " + projectRoot + " name=Family-Ops class=local_git_project default_branch=main"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(/project add) error = %v", err)
	}

	if !strings.Contains(output.String(), "Git repository") && !strings.Contains(output.String(), "Git repository") && !strings.Contains(output.String(), "must point at a Git repository") {
		t.Fatalf("output = %q, want git-root validation failure", output.String())
	}
	if _, ok := shell.env.Registry.Lookup("family-ops"); ok {
		t.Fatalf("family-ops should not be added after invalid git root")
	}

	configPath := registryConfigPath(t, shell.env.Registry)
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(config) error = %v", err)
	}
	if strings.Contains(string(content), "key: family-ops") {
		t.Fatalf("config = %q, did not expect family-ops entry", string(content))
	}
}

func TestShellSkillUsePersistsSelectionAndShowsInPrompt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project alpha) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/skill use pixel-perfect-ui-ux-designer", &output); err != nil {
		t.Fatalf("HandleLine(/skill use) error = %v", err)
	}

	if shell.state.SelectedSkillKey != "pixel-perfect-ui-ux-designer" {
		t.Fatalf("SelectedSkillKey = %q, want pixel-perfect-ui-ux-designer", shell.state.SelectedSkillKey)
	}

	output.Reset()
	if err := shell.renderPrompt(ctx, &output); err != nil {
		t.Fatalf("renderPrompt() error = %v", err)
	}
	if !strings.Contains(output.String(), "skill=pixel-perfect-ui-ux-designer") {
		t.Fatalf("prompt = %q, want selected skill in header", output.String())
	}
}

func TestActModeUsesSelectedSkillToEnrichExecutionPrompt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/project alpha",
		"/skill use pixel-perfect-ui-ux-designer",
		"/transition set limited_action allow=run_task confirm because enable odin task execution",
		"/mode act",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Audit the dashboard hierarchy", &output); err != nil {
		t.Fatalf("HandleLine(act input) error = %v", err)
	}
	if !strings.Contains(output.String(), "Pixel Perfect UI/UX Designer") {
		t.Fatalf("output = %q, want skill-enriched executor prompt", output.String())
	}

	output.Reset()
	if err := shell.HandleLine(ctx, "/runs show active", &output); err != nil {
		t.Fatalf("HandleLine(/runs show active) error = %v", err)
	}
	if !strings.Contains(output.String(), "Task Request:\nAudit the dashboard hierarchy") {
		t.Fatalf("run detail = %q, want enriched transcript prompt", output.String())
	}
}

func TestAskModeUsesSelectedSkillToEnrichConversationPrompt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/project alpha",
		"/skill use pixel-perfect-ui-ux-designer",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Audit the dashboard hierarchy", &output); err != nil {
		t.Fatalf("HandleLine(ask input) error = %v", err)
	}
	if !strings.Contains(output.String(), "Pixel Perfect UI/UX Designer") {
		t.Fatalf("output = %q, want skill-enriched ask prompt", output.String())
	}
	if !strings.Contains(output.String(), "Task Request:\nAudit the dashboard hierarchy") {
		t.Fatalf("output = %q, want enriched task request in ask response", output.String())
	}
}

func TestShellAgentListShowsRegistryAgents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/agent list", &output); err != nil {
		t.Fatalf("HandleLine(/agent list) error = %v", err)
	}
	if !strings.Contains(output.String(), "marcus-social-content-strategist-companion") {
		t.Fatalf("output = %q, want companion agent in list", output.String())
	}
}

func TestShellAgentShowRendersCompanionDetail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/agent show marcus-social-content-strategist-companion", &output); err != nil {
		t.Fatalf("HandleLine(/agent show) error = %v", err)
	}
	if !strings.Contains(output.String(), "role=social-content-strategist") {
		t.Fatalf("output = %q, want agent role", output.String())
	}
	if !strings.Contains(output.String(), "Purpose:") {
		t.Fatalf("output = %q, want rendered sections", output.String())
	}
}

func TestShellAgentRunCreatesDelegationsAndRendersRunEvidence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	manifest, ok := env.Registry.Lookup("alpha")
	if !ok {
		t.Fatal("expected alpha manifest")
	}
	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           manifest.Key,
		Name:          manifest.Name,
		Scope:         "project",
		GitRoot:       manifest.GitRoot,
		DefaultBranch: manifest.DefaultBranch,
		GitHubRepo:    manifest.GitHub.Repo,
		ManifestPath:  manifest.SourcePath,
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	transitionService := projects.Service{Store: env.Store}
	if _, err := transitionService.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:      project.ID,
		Actor:          projects.TransitionControllerOdinOS,
		TargetState:    projects.TransitionStateLimitedAction,
		LimitedActions: []string{"run_task"},
		ChangedBy:      "test",
		Notes:          "allow agent execution",
	}); err != nil {
		t.Fatalf("SetTransitionState(limited_action) error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project alpha) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/agent run portal-delivery-agent portal_track=student surface=dashboard goal=deliver-student-dashboard", &output); err != nil {
		t.Fatalf("HandleLine(/agent run) error = %v", err)
	}

	for _, want := range []string{"created task", "run=", "delegation=", "effective_skill=", "memory="} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellWorkflowValidateShowsReady(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/workflow validate marcus-social-growth-workflow", &output); err != nil {
		t.Fatalf("HandleLine(/workflow validate) error = %v", err)
	}
	if !strings.Contains(output.String(), "workflow=marcus-social-growth-workflow status=ready") {
		t.Fatalf("output = %q, want ready workflow", output.String())
	}
}

func TestShellWorkflowUsePersistsSelectionAndShowsInPrompt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output); err != nil {
		t.Fatalf("HandleLine(/workflow use) error = %v", err)
	}

	if shell.state.SelectedWorkflowKey != "marcus-social-growth-workflow" {
		t.Fatalf("SelectedWorkflowKey = %q, want marcus-social-growth-workflow", shell.state.SelectedWorkflowKey)
	}

	output.Reset()
	if err := shell.renderPrompt(ctx, &output); err != nil {
		t.Fatalf("renderPrompt() error = %v", err)
	}
	if !strings.Contains(output.String(), "workflow=marcus-social-growth-workflow") {
		t.Fatalf("prompt = %q, want selected workflow in header", output.String())
	}
}

func TestAskModeUsesSelectedWorkflowToEnrichConversationPrompt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output); err != nil {
		t.Fatalf("HandleLine(/workflow use) error = %v", err)
	}
	output.Reset()

	if err := shell.HandleLine(ctx, "Build next week's aviation content plan", &output); err != nil {
		t.Fatalf("HandleLine(ask input) error = %v", err)
	}
	if !strings.Contains(output.String(), "Marcus Social Growth Workflow") {
		t.Fatalf("output = %q, want workflow-enriched ask prompt", output.String())
	}
	if !strings.Contains(output.String(), "Workflow Composes:") {
		t.Fatalf("output = %q, want workflow composition context", output.String())
	}
	if !strings.Contains(output.String(), "Task Request:\nBuild next week's aviation content plan") {
		t.Fatalf("output = %q, want task request", output.String())
	}
}

func TestAskModeAnalyticsSkillIncludesRecentSocialRetrospectiveContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Now().UTC()
	for _, entry := range []struct {
		memoryType string
		summary    string
		fields     map[string]string
		createdAt  time.Time
	}{
		{
			memoryType: "social_outcome",
			summary:    "LinkedIn post approved for queue",
			fields:     map[string]string{"result": "approved", "channel": "linkedin", "content_kind": "post"},
			createdAt:  now.Add(-24 * time.Hour),
		},
		{
			memoryType: "social_outcome",
			summary:    "X reply rejected for tone",
			fields:     map[string]string{"result": "rejected", "channel": "x", "content_kind": "reply"},
			createdAt:  now.Add(-48 * time.Hour),
		},
		{
			memoryType: "social_learning",
			summary:    "Stronger inner-thought framing worked better than generic advice.",
			fields:     map[string]string{"channel": "x"},
			createdAt:  now.Add(-72 * time.Hour),
		},
		{
			memoryType: "social_research",
			summary:    "Airline training professionalism performed better than hustle language.",
			fields:     map[string]string{"channel": "linkedin"},
			createdAt:  now.Add(-96 * time.Hour),
		},
	} {
		recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", entry.memoryType, entry.summary, entry.fields, entry.createdAt)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(retrospective ask) error = %v", err)
	}
	for _, want := range []string{
		"Retrospective Window: last 7 days",
		"Recent Approved Outcomes:",
		"LinkedIn post approved for queue",
		"Recent Rejected Outcomes:",
		"X reply rejected for tone",
		"Recent Learnings:",
		"Stronger inner-thought framing worked better than generic advice.",
		"Recent Research Signals:",
		"Airline training professionalism performed better than hustle language.",
		"X Voice Guidance:",
		"inner thoughts",
		"LinkedIn Voice Guidance:",
		"professional framing",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestAskModeAnalyticsSkillIncludesRecentXVisibleEvidence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", "social_evidence", "Visible X evidence captured for Marcus crosswind coaching post.", map[string]string{
		"channel":         "x",
		"evidence_kind":   "x_post_visible",
		"target_url":      "https://x.com/marcus/status/123",
		"author_handle":   "@marcus",
		"reply_count":     "4",
		"repost_count":    "2",
		"like_count":      "18",
		"bookmark_count":  "1",
		"view_count":      "1400",
		"screenshot_path": "/tmp/marcus-crosswind.png",
	}, time.Now().UTC().Add(-24*time.Hour))

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(retrospective ask) error = %v", err)
	}
	for _, want := range []string{
		"Latest X Visible Evidence Snapshot:",
		"@marcus",
		"replies=4",
		"reposts=2",
		"likes=18",
		"bookmarks=1",
		"views=1400",
		"Visible X evidence captured for Marcus crosswind coaching post.",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestAskModeAnalyticsSkillHighlightsLatestXVisibleEvidenceSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", "social_evidence", "Older visible X evidence captured for Marcus crosswind coaching post.", map[string]string{
		"channel":        "x",
		"evidence_kind":  "x_post_visible",
		"target_url":     "https://x.com/marcus/status/123",
		"author_handle":  "@marcus",
		"view_count":     "3 Views",
		"bookmark_count": "",
	}, time.Now().UTC().Add(-2*time.Hour))
	recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", "social_evidence", "Newest visible X evidence captured for Marcus crosswind coaching post.", map[string]string{
		"channel":        "x",
		"evidence_kind":  "x_post_visible",
		"target_url":     "https://x.com/marcus/status/123",
		"author_handle":  "@marcus",
		"view_count":     "4 Views",
		"bookmark_count": "",
	}, time.Now().UTC().Add(-1*time.Hour))

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(retrospective ask) error = %v", err)
	}
	for _, want := range []string{
		"Latest X Visible Evidence Snapshot:",
		"- [x] @marcus views=4 Views Newest visible X evidence captured for Marcus crosswind coaching post.",
		"Recent X Visible Evidence History:",
		"- [x] @marcus views=3 Views Older visible X evidence captured for Marcus crosswind coaching post.",
		"Use the Latest X Visible Evidence Snapshot entry as the canonical most recent visible evidence.",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestAskModeAnalyticsSkillIncludesBundledXVisibleEvidence(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "x-bundle-driver.sh")
	script := `#!/usr/bin/env bash
request_path="$(mktemp)"
cat >"$request_path"
python3 - "$request_path" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    request = json.load(handle)
target_url = (((request.get("input") or {}).get("target_url")) or "").strip()
suffix = target_url.rsplit("/", 1)[-1] or "post"
print(json.dumps({
    "status": "completed",
    "tool_key": "browser_x_post_visible_evidence",
    "summary": f"Captured visible X post evidence for weekly-review-{suffix}.",
    "artifacts": {
        "target_url": target_url,
        "final_url": target_url,
        "label": f"weekly-review-{suffix}",
        "title": "X",
        "screenshot_path": f"/tmp/{suffix}.png",
        "snapshot_path": f"/tmp/{suffix}.txt",
        "snapshot_excerpt": f"Excerpt for {suffix}",
        "post_text": f"Post text for {suffix}",
        "author_display_name": "Marcus Gollahon",
        "author_handle": "@marcus",
        "reply_count": "4",
        "repost_count": "2",
        "like_count": "18",
        "view_count": "1400"
    }
}))
PY
rm -f "$request_path"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", scriptPath)

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/tool run browser_x_weekly_evidence_bundle target_urls=https://x.com/marcus/status/123,https://x.com/marcus/status/456 label=weekly-review",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(retrospective ask) error = %v", err)
	}
	for _, want := range []string{
		"Latest X Visible Evidence Snapshot:",
		"Captured visible X post evidence for weekly-review-123.",
		"Captured visible X post evidence for weekly-review-456.",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestAskModeAnalyticsSkillSkipsOlderSocialMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", "social_outcome", "Older approved outcome", map[string]string{
		"result":       "approved",
		"channel":      "linkedin",
		"content_kind": "post",
	}, time.Now().UTC().Add(-8*24*time.Hour))

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(retrospective ask) error = %v", err)
	}
	if strings.Contains(output.String(), "Older approved outcome") {
		t.Fatalf("output = %q, want older memory excluded", output.String())
	}
}

func TestAskModeAnalyticsSkillDoesNotIncludeSocialDrafts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", "social_draft", "Draft that should not appear in retrospective", map[string]string{
		"channel":  "x",
		"approval": "pending",
	}, time.Now().UTC().Add(-12*time.Hour))

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(retrospective ask) error = %v", err)
	}
	if strings.Contains(output.String(), "Draft that should not appear in retrospective") {
		t.Fatalf("output = %q, want social_draft excluded", output.String())
	}
}

func TestAskModeAnalyticsSkillIncludesMultiWeekComparison(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Now().UTC()
	for _, entry := range []struct {
		memoryType string
		summary    string
		fields     map[string]string
		createdAt  time.Time
	}{
		{
			memoryType: "social_outcome",
			summary:    "Current-week LinkedIn post approved",
			fields:     map[string]string{"result": "approved", "channel": "linkedin", "content_kind": "post"},
			createdAt:  now.Add(-24 * time.Hour),
		},
		{
			memoryType: "social_outcome",
			summary:    "Prior-week LinkedIn post approved",
			fields:     map[string]string{"result": "approved", "channel": "linkedin", "content_kind": "post"},
			createdAt:  now.Add(-8 * 24 * time.Hour),
		},
		{
			memoryType: "social_outcome",
			summary:    "Current-week X reply rejected",
			fields:     map[string]string{"result": "rejected", "channel": "x", "content_kind": "reply"},
			createdAt:  now.Add(-48 * time.Hour),
		},
		{
			memoryType: "social_outcome",
			summary:    "Prior-week X reply rejected",
			fields:     map[string]string{"result": "rejected", "channel": "x", "content_kind": "reply"},
			createdAt:  now.Add(-9 * 24 * time.Hour),
		},
		{
			memoryType: "social_learning",
			summary:    "Stronger inner-thought framing beats generic advice.",
			fields:     map[string]string{"channel": "x"},
			createdAt:  now.Add(-72 * time.Hour),
		},
		{
			memoryType: "social_learning",
			summary:    "Stronger inner-thought framing beats generic advice.",
			fields:     map[string]string{"channel": "x"},
			createdAt:  now.Add(-10 * 24 * time.Hour),
		},
		{
			memoryType: "social_research",
			summary:    "Professional debrief framing performs better on LinkedIn.",
			fields:     map[string]string{"channel": "linkedin"},
			createdAt:  now.Add(-96 * time.Hour),
		},
		{
			memoryType: "social_research",
			summary:    "Professional debrief framing performs better on LinkedIn.",
			fields:     map[string]string{"channel": "linkedin"},
			createdAt:  now.Add(-16 * 24 * time.Hour),
		},
		{
			memoryType: "social_learning",
			summary:    "Mentorship angle felt more credible than career hype.",
			fields:     map[string]string{"channel": "linkedin"},
			createdAt:  now.Add(-12 * time.Hour),
		},
		{
			memoryType: "social_outcome",
			summary:    "Older window X thread approved",
			fields:     map[string]string{"result": "approved", "channel": "x", "content_kind": "thread"},
			createdAt:  now.Add(-15 * 24 * time.Hour),
		},
		{
			memoryType: "social_outcome",
			summary:    "Oldest window LinkedIn article seed approved",
			fields:     map[string]string{"result": "approved", "channel": "linkedin", "content_kind": "article_seed"},
			createdAt:  now.Add(-23 * 24 * time.Hour),
		},
	} {
		recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", entry.memoryType, entry.summary, entry.fields, entry.createdAt)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(retrospective ask) error = %v", err)
	}
	for _, want := range []string{
		"Retrospective Window: last 7 days",
		"Comparison Window: last 4 weekly windows",
		"Week 1 (",
		"Week 2 (",
		"Week 3 (",
		"Week 4 (",
		"Recurring Approval Patterns:",
		"- linkedin/post approved",
		"Recurring Rejection Patterns:",
		"- x/reply rejected",
		"Recurring Learning Signals:",
		"- [x] Stronger inner-thought framing beats generic advice.",
		"Recurring Research Signals:",
		"- [linkedin] Professional debrief framing performs better on LinkedIn.",
		"New This Week:",
		"- [linkedin] Mentorship angle felt more credible than career hype.",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestAskModeAnalyticsSkillMultiWeekComparisonDegradesGracefully(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", "social_learning", "Fresh current-week learning", map[string]string{
		"channel": "x",
	}, time.Now().UTC().Add(-24*time.Hour))

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(retrospective ask) error = %v", err)
	}
	for _, want := range []string{
		"Comparison Window: last 4 weekly windows",
		"Recurring Approval Patterns:\n- none",
		"Recurring Rejection Patterns:\n- none",
		"Recurring Research Signals:\n- none",
		"New This Week:",
		"- [x] Fresh current-week learning",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestAskModeAnalyticsSkillProvidesCarryForwardGuidance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Now().UTC()
	for _, entry := range []struct {
		memoryType string
		summary    string
		fields     map[string]string
		createdAt  time.Time
	}{
		{
			memoryType: "social_outcome",
			summary:    "Current-week LinkedIn post approved",
			fields:     map[string]string{"result": "approved", "channel": "linkedin", "content_kind": "post"},
			createdAt:  now.Add(-24 * time.Hour),
		},
		{
			memoryType: "social_outcome",
			summary:    "Prior-week LinkedIn post approved",
			fields:     map[string]string{"result": "approved", "channel": "linkedin", "content_kind": "post"},
			createdAt:  now.Add(-8 * 24 * time.Hour),
		},
		{
			memoryType: "social_outcome",
			summary:    "Current-week X reply rejected",
			fields:     map[string]string{"result": "rejected", "channel": "x", "content_kind": "reply"},
			createdAt:  now.Add(-48 * time.Hour),
		},
		{
			memoryType: "social_outcome",
			summary:    "Prior-week X reply rejected",
			fields:     map[string]string{"result": "rejected", "channel": "x", "content_kind": "reply"},
			createdAt:  now.Add(-9 * 24 * time.Hour),
		},
		{
			memoryType: "social_learning",
			summary:    "Stronger inner-thought framing beats generic advice.",
			fields:     map[string]string{"channel": "x"},
			createdAt:  now.Add(-72 * time.Hour),
		},
		{
			memoryType: "social_learning",
			summary:    "Stronger inner-thought framing beats generic advice.",
			fields:     map[string]string{"channel": "x"},
			createdAt:  now.Add(-10 * 24 * time.Hour),
		},
		{
			memoryType: "social_research",
			summary:    "Professional debrief framing performs better on LinkedIn.",
			fields:     map[string]string{"channel": "linkedin"},
			createdAt:  now.Add(-96 * time.Hour),
		},
		{
			memoryType: "social_research",
			summary:    "Professional debrief framing performs better on LinkedIn.",
			fields:     map[string]string{"channel": "linkedin"},
			createdAt:  now.Add(-16 * 24 * time.Hour),
		},
		{
			memoryType: "social_learning",
			summary:    "Mentorship angle felt more credible than career hype.",
			fields:     map[string]string{"channel": "linkedin"},
			createdAt:  now.Add(-12 * time.Hour),
		},
	} {
		recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", entry.memoryType, entry.summary, entry.fields, entry.createdAt)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(retrospective ask) error = %v", err)
	}
	for _, want := range []string{
		"Next-Week Carry-Forward",
		"Keep:",
		"- linkedin/post approved",
		"Avoid:",
		"- x/reply rejected",
		"Test Next:",
		"- [x] Stronger inner-thought framing beats generic advice.",
		"- [linkedin] Professional debrief framing performs better on LinkedIn.",
		"- [linkedin] Mentorship angle felt more credible than career hype.",
		"X Direction:",
		"inner thoughts",
		"LinkedIn Direction:",
		"professionally framed",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestAskModeAnalyticsSkillCarryForwardGuidanceDegradesGracefully(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", "social_learning", "Fresh current-week learning", map[string]string{
		"channel": "x",
	}, time.Now().UTC().Add(-24*time.Hour))

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(retrospective ask) error = %v", err)
	}
	for _, want := range []string{
		"Next-Week Carry-Forward",
		"Keep:\n- none",
		"Avoid:\n- none",
		"Test Next:",
		"- [x] Fresh current-week learning",
		"X Direction:",
		"inner thoughts",
		"LinkedIn Direction:",
		"professionally framed",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestAskModeNonAnalyticsSkillDoesNotAutoInjectRetrospectiveContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recordWorkflowMemoryAtTime(t, ctx, env, "marcus-social-growth-workflow", "social_outcome", "Approved memory that should stay out of drafting prompt", map[string]string{
		"result":       "approved",
		"channel":      "linkedin",
		"content_kind": "post",
	}, time.Now().UTC().Add(-24*time.Hour))

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-x-drafting-assistant",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Draft tomorrow's X idea.", &output); err != nil {
		t.Fatalf("HandleLine(non-analytics ask) error = %v", err)
	}
	if strings.Contains(output.String(), "Retrospective Window: last 7 days") {
		t.Fatalf("output = %q, want no retrospective injection", output.String())
	}
	if strings.Contains(output.String(), "Approved memory that should stay out of drafting prompt") {
		t.Fatalf("output = %q, want no social retrospective memory injection", output.String())
	}
}

func TestShellAskAutoRecordsPendingSocialDraftForXDraftingSkill(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-x-drafting-assistant",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Draft an X post about debrief discipline after crosswind lessons.", &output); err != nil {
		t.Fatalf("HandleLine(drafting ask) error = %v", err)
	}

	summaries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("memory summaries len = %d, want 1", len(summaries))
	}
	if !strings.Contains(summaries[0].Summary, "codex_headless completed") {
		t.Fatalf("summary = %q, want executor-backed draft output", summaries[0].Summary)
	}
	for _, want := range []string{
		`"selected_workflow_key":"marcus-social-growth-workflow"`,
		`"selected_skill_key":"marcus-x-drafting-assistant"`,
		`"approval":"pending"`,
		`"channel":"x"`,
		`"content_kind":"post"`,
	} {
		if !strings.Contains(summaries[0].DetailsJSON, want) {
			t.Fatalf("details_json missing %q: %s", want, summaries[0].DetailsJSON)
		}
	}
}

func TestShellAskAutoRecordsPendingSocialDraftForEngagementResearchSkill(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-engagement-research-assistant",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Review this X post and draft one compliant X reply suggestion: A CFI should correct crosswind drift before touchdown, not after.", &output); err != nil {
		t.Fatalf("HandleLine(engagement ask) error = %v", err)
	}

	summaries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("memory summaries len = %d, want 1", len(summaries))
	}
	if !strings.Contains(summaries[0].Summary, "codex_headless completed") {
		t.Fatalf("summary = %q, want executor-backed reply suggestion output", summaries[0].Summary)
	}
	for _, want := range []string{
		`"selected_workflow_key":"marcus-social-growth-workflow"`,
		`"selected_skill_key":"marcus-engagement-research-assistant"`,
		`"approval":"pending"`,
		`"artifact_kind":"reply_suggestion"`,
		`"channel":"x"`,
		`"content_kind":"reply"`,
	} {
		if !strings.Contains(summaries[0].DetailsJSON, want) {
			t.Fatalf("details_json missing %q: %s", want, summaries[0].DetailsJSON)
		}
	}
}

func TestShellAskAutoRecordsPendingSocialDraftForEngagementResearchSkillWithReplyTargetURL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-engagement-research-assistant",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	prompt := "Review this X post https://x.com/marcusgoll/status/2047122848230134153 and draft one compliant X reply suggestion that adds value without sounding defensive."
	if err := shell.HandleLine(ctx, prompt, &output); err != nil {
		t.Fatalf("HandleLine(engagement ask with target) error = %v", err)
	}

	summaries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("memory summaries len = %d, want 1", len(summaries))
	}
	if !strings.Contains(summaries[0].DetailsJSON, `"in_reply_to_url":"https://x.com/marcusgoll/status/2047122848230134153"`) {
		t.Fatalf("details_json = %s, want in_reply_to_url", summaries[0].DetailsJSON)
	}
}

func TestSocialDraftFieldsForSelectedSkillKeepsPostWhenPromptNegatesThread(t *testing.T) {
	t.Parallel()

	fields, ok := socialDraftFieldsForSelectedSkill(
		"marcus-social-growth-workflow",
		"marcus-x-drafting-assistant",
		"Draft one primary X post only. Keep it to one concise X post, not a thread.",
	)
	if !ok {
		t.Fatal("ok = false, want social draft fields")
	}
	if got := fields["content_kind"]; got != "post" {
		t.Fatalf("content_kind = %q, want post", got)
	}
}

func TestSocialDraftFieldsForSelectedSkillMarksThreadForAffirmativeThreadRequest(t *testing.T) {
	t.Parallel()

	fields, ok := socialDraftFieldsForSelectedSkill(
		"marcus-social-growth-workflow",
		"marcus-x-drafting-assistant",
		"Draft a short thread about crosswind debrief discipline.",
	)
	if !ok {
		t.Fatal("ok = false, want social draft fields")
	}
	if got := fields["content_kind"]; got != "thread" {
		t.Fatalf("content_kind = %q, want thread", got)
	}
}

func TestSocialDraftFieldsForSelectedSkillMarksReplySuggestionForEngagementResearchOnX(t *testing.T) {
	t.Parallel()

	fields, ok := socialDraftFieldsForSelectedSkill(
		"marcus-social-growth-workflow",
		"marcus-engagement-research-assistant",
		"Review this X post and draft one compliant X reply suggestion for it.",
	)
	if !ok {
		t.Fatal("ok = false, want social draft fields")
	}
	for key, want := range map[string]string{
		"approval":      "pending",
		"artifact_kind": "reply_suggestion",
		"channel":       "x",
		"content_kind":  "reply",
	} {
		if got := fields[key]; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestSocialDraftFieldsForSelectedSkillCapturesReplyTargetForEngagementResearchOnX(t *testing.T) {
	t.Parallel()

	fields, ok := socialDraftFieldsForSelectedSkill(
		"marcus-social-growth-workflow",
		"marcus-engagement-research-assistant",
		"Review this X post https://x.com/marcusgoll/status/2047122848230134153 and draft one compliant X reply suggestion for it.",
	)
	if !ok {
		t.Fatal("ok = false, want social draft fields")
	}
	if got := fields["in_reply_to_url"]; got != "https://x.com/marcusgoll/status/2047122848230134153" {
		t.Fatalf("in_reply_to_url = %q, want target URL", got)
	}
}

func TestApprovedOutcomePublishTextStripsApprovalChecklist(t *testing.T) {
	t.Parallel()

	summary := `If the debrief starts at the flare, you're already late.

Approval checklist:
- Matches your normal instructor voice
- No factual changes needed before posting`

	if got, want := approvedOutcomePublishText(summary), "If the debrief starts at the flare, you're already late."; got != want {
		t.Fatalf("approvedOutcomePublishText() = %q, want %q", got, want)
	}
}

func TestApprovedOutcomePublishTextKeepsPlainPost(t *testing.T) {
	t.Parallel()

	summary := "Approved X post ready to publish natively."
	if got := approvedOutcomePublishText(summary); got != summary {
		t.Fatalf("approvedOutcomePublishText() = %q, want %q", got, summary)
	}
}

func TestShellAskDoesNotAutoRecordSocialDraftForAnalyticsSkill(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/skill use marcus-social-analytics-advisor",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "Give me this week's retrospective.", &output); err != nil {
		t.Fatalf("HandleLine(analytics ask) error = %v", err)
	}

	summaries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("memory summaries len = %d, want 0", len(summaries))
	}
}

func TestShellMemoryRememberRecordsWorkflowScopedEntry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_draft channel=x approval=pending -- Draft about coaching students through crosswind landing plateaus.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
	}

	if !strings.Contains(output.String(), "type=social_draft") {
		t.Fatalf("output = %q, want recorded memory type", output.String())
	}
	if !strings.Contains(output.String(), "scope=workflow/marcus-social-growth-workflow") {
		t.Fatalf("output = %q, want workflow memory scope", output.String())
	}

	summaries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:    "workflow",
		ScopeKey: "marcus-social-growth-workflow",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("memory summaries len = %d, want 1", len(summaries))
	}
	if summaries[0].MemoryType != "social_draft" {
		t.Fatalf("MemoryType = %q, want social_draft", summaries[0].MemoryType)
	}
	if !strings.Contains(summaries[0].DetailsJSON, `"selected_workflow_key":"marcus-social-growth-workflow"`) {
		t.Fatalf("DetailsJSON = %q, want selected workflow", summaries[0].DetailsJSON)
	}
	if !strings.Contains(summaries[0].DetailsJSON, `"channel":"x"`) {
		t.Fatalf("DetailsJSON = %q, want channel field", summaries[0].DetailsJSON)
	}
}

func TestShellMemoryListShowsWorkflowEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_outcome result=approved channel=linkedin content_kind=post -- LinkedIn draft approved for next review batch.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "/memory list type=social_outcome", &output); err != nil {
		t.Fatalf("HandleLine(/memory list) error = %v", err)
	}
	if !strings.Contains(output.String(), "scope=workflow/marcus-social-growth-workflow") {
		t.Fatalf("output = %q, want workflow scope", output.String())
	}
	if !strings.Contains(output.String(), "summary=LinkedIn draft approved for next review batch.") {
		t.Fatalf("output = %q, want memory summary", output.String())
	}
	if !strings.Contains(output.String(), `"result":"approved"`) {
		t.Fatalf("output = %q, want structured details", output.String())
	}
}

func TestShellMemoryListSeparatesApprovedAndRejectedSocialOutcomes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_outcome result=approved channel=linkedin content_kind=post -- Approved LinkedIn post.",
		"/memory remember social_outcome result=rejected channel=x content_kind=reply reason=too-defensive -- Rejected X reply.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "/memory list type=social_outcome field.result=approved", &output); err != nil {
		t.Fatalf("HandleLine(/memory list approved) error = %v", err)
	}
	if !strings.Contains(output.String(), "Approved LinkedIn post.") {
		t.Fatalf("output = %q, want approved outcome", output.String())
	}
	if strings.Contains(output.String(), "Rejected X reply.") {
		t.Fatalf("output = %q, want rejected outcome filtered out", output.String())
	}

	output.Reset()
	if err := shell.HandleLine(ctx, "/memory list type=social_outcome field.result=rejected", &output); err != nil {
		t.Fatalf("HandleLine(/memory list rejected) error = %v", err)
	}
	if !strings.Contains(output.String(), "Rejected X reply.") {
		t.Fatalf("output = %q, want rejected outcome", output.String())
	}
	if strings.Contains(output.String(), "Approved LinkedIn post.") {
		t.Fatalf("output = %q, want approved outcome filtered out", output.String())
	}
}

func TestShellMemoryRememberRejectsSocialOutcomeMissingRequiredFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output); err != nil {
		t.Fatalf("HandleLine(/workflow use) error = %v", err)
	}
	output.Reset()

	if err := shell.HandleLine(ctx, "/memory remember social_outcome channel=linkedin -- Missing result and content kind.", &output); err != nil {
		t.Fatalf("HandleLine(/memory remember social_outcome) error = %v", err)
	}
	if !strings.Contains(output.String(), "social_outcome requires result=approved|rejected") {
		t.Fatalf("output = %q, want validation error", output.String())
	}

	summaries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:    "workflow",
		ScopeKey: "marcus-social-growth-workflow",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("memory summaries len = %d, want 0", len(summaries))
	}
}

func TestShellMemoryRememberRejectsSocialOutcomeInvalidValues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output); err != nil {
		t.Fatalf("HandleLine(/workflow use) error = %v", err)
	}
	output.Reset()

	for _, command := range []string{
		"/memory remember social_outcome result=queued channel=linkedin content_kind=post -- Invalid result.",
		"/memory remember social_outcome result=approved channel=threads content_kind=post -- Invalid channel.",
		"/memory remember social_outcome result=approved channel=linkedin content_kind=comment -- Invalid content kind.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
	}
	for _, want := range []string{
		"social_outcome requires result=approved|rejected",
		"social_outcome requires channel=x|linkedin",
		"social_outcome requires content_kind=post|reply|thread|article_seed",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
}

func TestShellMemoryShowDisplaysSingleWorkflowEntry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_draft channel=x approval=pending -- Draft A",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	summaries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:    "workflow",
		ScopeKey: "marcus-social-growth-workflow",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("memory summaries len = %d, want 1", len(summaries))
	}

	if err := shell.HandleLine(ctx, "/memory show "+strconv.FormatInt(summaries[0].ID, 10), &output); err != nil {
		t.Fatalf("HandleLine(/memory show) error = %v", err)
	}
	if !strings.Contains(output.String(), "memory="+strconv.FormatInt(summaries[0].ID, 10)) {
		t.Fatalf("output = %q, want memory id", output.String())
	}
	if !strings.Contains(output.String(), "summary=Draft A") {
		t.Fatalf("output = %q, want memory summary", output.String())
	}
	if !strings.Contains(output.String(), "fields=approval=pending,channel=x") {
		t.Fatalf("output = %q, want rendered structured fields", output.String())
	}
}

func TestShellMemoryListFiltersExecutionMetadataFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	if _, err := env.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:   &project.ID,
		Scope:       "project",
		ScopeKey:    "alpha",
		MemoryType:  "episode",
		Summary:     "portal-delivery-agent coordinated 5 child delegations for admin-cfi dashboard",
		DetailsJSON: `{"task_key":"portal-delivery-agent-deliver-admin-dashboard-123","task_status":"completed","run_status":"completed","executor":"portal-delivery-agent","execution_metadata":{"agent_key":"portal-delivery-agent","portal_track":"admin-cfi","requested_skill":"pixel-perfect-ui-ux-designer","effective_skill":"pixel-perfect-ui-ux-designer","skill_source":"agent_template"}}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project alpha) error = %v", err)
	}
	output.Reset()

	command := "/memory list type=episode field.agent_key=portal-delivery-agent field.portal_track=admin-cfi field.effective_skill=pixel-perfect-ui-ux-designer"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", command, err)
	}
	if !strings.Contains(output.String(), "portal-delivery-agent coordinated 5 child delegations for admin-cfi dashboard") {
		t.Fatalf("output = %q, want execution memory summary", output.String())
	}
	if !strings.Contains(output.String(), "fields=agent_key=portal-delivery-agent") {
		t.Fatalf("output = %q, want rendered execution metadata fields", output.String())
	}
}

func TestShellMemoryListFiltersFlatExecutionFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	project, err := env.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(alpha) error = %v", err)
	}
	if _, err := env.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:   &project.ID,
		Scope:       "project",
		ScopeKey:    "alpha",
		MemoryType:  "episode",
		Summary:     "portal-delivery-agent coordinated 5 child delegations for admin-cfi dashboard",
		DetailsJSON: `{"agent_key":"portal-delivery-agent","child_delegation_ids":[20,21,22,23,24],"learning_proposal_ids":[4],"portal_track":"admin-cfi","run_status":"completed","surface":"dashboard","task_key":"portal-delivery-agent-deliver-admin-dashboard-123","task_status":"completed"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project alpha) error = %v", err)
	}
	output.Reset()

	command := "/memory list type=episode field.agent_key=portal-delivery-agent field.portal_track=admin-cfi field.surface=dashboard"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", command, err)
	}
	if !strings.Contains(output.String(), "portal-delivery-agent coordinated 5 child delegations for admin-cfi dashboard") {
		t.Fatalf("output = %q, want parent execution memory summary", output.String())
	}
	if !strings.Contains(output.String(), "fields=agent_key=portal-delivery-agent") {
		t.Fatalf("output = %q, want rendered flat execution fields", output.String())
	}
}

func TestShellMemoryShowRejectsInvisibleEntry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	globalSummary, err := env.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       "global",
		ScopeKey:    "global",
		MemoryType:  "social_draft",
		Summary:     "Global draft",
		DetailsJSON: `{"source":"test","fields":{"channel":"x"}}`,
	})
	if err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output); err != nil {
		t.Fatalf("HandleLine(/workflow use) error = %v", err)
	}
	output.Reset()

	if err := shell.HandleLine(ctx, "/memory show "+strconv.FormatInt(globalSummary.ID, 10), &output); err != nil {
		t.Fatalf("HandleLine(/memory show) error = %v", err)
	}
	if !strings.Contains(output.String(), "unknown memory") {
		t.Fatalf("output = %q, want unknown memory", output.String())
	}
}

func TestShellMemoryListFiltersByFieldContainsAndLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_draft channel=x approval=pending -- Crosswind draft",
		"/memory remember social_draft channel=x approval=approved -- Crosswind published",
		"/memory remember social_draft channel=linkedin approval=pending -- LinkedIn draft",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "/memory list type=social_draft field.approval=pending contains=Crosswind limit=1 order=desc", &output); err != nil {
		t.Fatalf("HandleLine(/memory list filtered) error = %v", err)
	}
	if !strings.Contains(output.String(), "summary=Crosswind draft") {
		t.Fatalf("output = %q, want pending crosswind draft", output.String())
	}
	if strings.Contains(output.String(), "Crosswind published") {
		t.Fatalf("output = %q, want approved entry filtered out", output.String())
	}
	if strings.Contains(output.String(), "LinkedIn draft") {
		t.Fatalf("output = %q, want contains filter to exclude unrelated draft", output.String())
	}
}

func TestShellMemoryListOrdersDescendingWhenRequested(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_learning channel=x -- First learning",
		"/memory remember social_learning channel=x -- Second learning",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "/memory list type=social_learning order=desc limit=1", &output); err != nil {
		t.Fatalf("HandleLine(/memory list order=desc) error = %v", err)
	}
	if !strings.Contains(output.String(), "summary=Second learning") {
		t.Fatalf("output = %q, want newest entry first", output.String())
	}
	if strings.Contains(output.String(), "First learning") {
		t.Fatalf("output = %q, want older entry excluded by limit", output.String())
	}
}

func TestShellMemoryListSupportsContainsWithoutType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_research channel=x -- Crosswind questions keep resurfacing in public threads.",
		"/memory remember social_learning channel=linkedin -- Debrief structure matters more than hype.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	if err := shell.HandleLine(ctx, "/memory list contains=Crosswind", &output); err != nil {
		t.Fatalf("HandleLine(/memory list contains=...) error = %v", err)
	}
	if !strings.Contains(output.String(), "Crosswind questions keep resurfacing") {
		t.Fatalf("output = %q, want matching summary", output.String())
	}
	if strings.Contains(output.String(), "Debrief structure matters more than hype.") {
		t.Fatalf("output = %q, want non-matching summary excluded", output.String())
	}
}

func TestShellMemoryResolveApprovedDraftRecordsOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_draft channel=x content_kind=post approval=pending -- Debriefs should start with the decision chain, not the touchdown.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	drafts, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(drafts) error = %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("draft len = %d, want 1", len(drafts))
	}

	resolveCommand := "/memory resolve " + strconv.FormatInt(drafts[0].ID, 10) + " result=approved"
	if err := shell.HandleLine(ctx, resolveCommand, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", resolveCommand, err)
	}
	if !strings.Contains(output.String(), "status=resolved") {
		t.Fatalf("output = %q, want resolved status", output.String())
	}
	if !strings.Contains(output.String(), "outcome_memory=") {
		t.Fatalf("output = %q, want recorded outcome memory", output.String())
	}
	if !strings.Contains(output.String(), "result=approved") {
		t.Fatalf("output = %q, want approved outcome", output.String())
	}

	output.Reset()
	if err := shell.HandleLine(ctx, "/memory list type=social_draft field.approval=pending", &output); err != nil {
		t.Fatalf("HandleLine(/memory list pending) error = %v", err)
	}
	if !strings.Contains(output.String(), "no memory") {
		t.Fatalf("output = %q, want empty pending queue", output.String())
	}

	drafts, err = env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(updated drafts) error = %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("updated draft len = %d, want 1", len(drafts))
	}
	draftDetails, err := parseMemoryDetails(drafts[0].DetailsJSON)
	if err != nil {
		t.Fatalf("parseMemoryDetails(draft) error = %v", err)
	}
	if got := draftDetails.Fields["approval"]; got != "approved" {
		t.Fatalf("draft approval = %q, want approved", got)
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}
	outcomeDetails, err := parseMemoryDetails(outcomes[0].DetailsJSON)
	if err != nil {
		t.Fatalf("parseMemoryDetails(outcome) error = %v", err)
	}
	for key, want := range map[string]string{
		"channel":      "x",
		"content_kind": "post",
		"result":       "approved",
	} {
		if got := outcomeDetails.Fields[key]; got != want {
			t.Fatalf("outcome field %s = %q, want %q", key, got, want)
		}
	}
	if outcomes[0].Summary != drafts[0].Summary {
		t.Fatalf("outcome summary = %q, want %q", outcomes[0].Summary, drafts[0].Summary)
	}
}

func TestShellMemoryResolveApprovedReplyDraftPreservesReplyTargetOnOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_draft channel=x content_kind=reply approval=pending in_reply_to_url=https://x.com/example/status/123 -- Short, useful reply text.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	drafts, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(drafts) error = %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("draft len = %d, want 1", len(drafts))
	}

	resolveCommand := "/memory resolve " + strconv.FormatInt(drafts[0].ID, 10) + " result=approved"
	if err := shell.HandleLine(ctx, resolveCommand, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", resolveCommand, err)
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}

	outcomeDetails, err := parseMemoryDetails(outcomes[0].DetailsJSON)
	if err != nil {
		t.Fatalf("parseMemoryDetails(outcome) error = %v", err)
	}
	if got := outcomeDetails.Fields["in_reply_to_url"]; got != "https://x.com/example/status/123" {
		t.Fatalf("outcome in_reply_to_url = %q, want preserved target URL", got)
	}
}

func TestShellMemoryResolveRejectedDraftRecordsReason(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_draft channel=linkedin content_kind=post approval=pending -- Draft sounded too polished and not enough like Marcus.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	drafts, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(drafts) error = %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("draft len = %d, want 1", len(drafts))
	}

	resolveCommand := "/memory resolve " + strconv.FormatInt(drafts[0].ID, 10) + " result=rejected reason=too-generic"
	if err := shell.HandleLine(ctx, resolveCommand, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", resolveCommand, err)
	}
	if !strings.Contains(output.String(), "result=rejected") {
		t.Fatalf("output = %q, want rejected result", output.String())
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}
	outcomeDetails, err := parseMemoryDetails(outcomes[0].DetailsJSON)
	if err != nil {
		t.Fatalf("parseMemoryDetails(outcome) error = %v", err)
	}
	if got := outcomeDetails.Fields["reason"]; got != "too-generic" {
		t.Fatalf("outcome reason = %q, want too-generic", got)
	}
	if got := outcomeDetails.Fields["result"]; got != "rejected" {
		t.Fatalf("outcome result = %q, want rejected", got)
	}
}

func TestShellMemoryResolveRejectsAlreadyResolvedDraft(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_draft channel=x content_kind=post approval=approved -- Already resolved draft.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	drafts, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_draft",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(drafts) error = %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("draft len = %d, want 1", len(drafts))
	}

	resolveCommand := "/memory resolve " + strconv.FormatInt(drafts[0].ID, 10) + " result=approved"
	if err := shell.HandleLine(ctx, resolveCommand, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", resolveCommand, err)
	}
	if !strings.Contains(output.String(), "social_draft approval must be pending to resolve") {
		t.Fatalf("output = %q, want pending-only validation", output.String())
	}
}

func TestShellMemoryPublishMarksApprovedOutcomePublished(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_outcome result=approved channel=x content_kind=post -- Approved X post ready to publish.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}

	command := "/memory publish " + strconv.FormatInt(outcomes[0].ID, 10) + " url=https://x.com/marcus/status/123456789"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", command, err)
	}
	if !strings.Contains(output.String(), "status=published") {
		t.Fatalf("output = %q, want published status", output.String())
	}
	if !strings.Contains(output.String(), "publish_url=https://x.com/marcus/status/123456789") {
		t.Fatalf("output = %q, want publish URL field", output.String())
	}

	outcomes, err = env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(updated outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("updated outcome len = %d, want 1", len(outcomes))
	}
	details, err := parseMemoryDetails(outcomes[0].DetailsJSON)
	if err != nil {
		t.Fatalf("parseMemoryDetails(outcome) error = %v", err)
	}
	if got := details.Fields["publish_status"]; got != "published" {
		t.Fatalf("publish_status = %q, want published", got)
	}
	if got := details.Fields["publish_url"]; got != "https://x.com/marcus/status/123456789" {
		t.Fatalf("publish_url = %q, want expected URL", got)
	}
	if strings.TrimSpace(details.Fields["published_at"]) == "" {
		t.Fatalf("published_at = %q, want timestamp", details.Fields["published_at"])
	}
}

func TestParseMemoryPublishArgsSupportsHuginnXMode(t *testing.T) {
	t.Parallel()

	request, err := parseMemoryPublishArgs([]string{"12", "via=huginn_x"})
	if err != nil {
		t.Fatalf("parseMemoryPublishArgs(via=huginn_x) error = %v", err)
	}
	if request.MemoryID != 12 {
		t.Fatalf("MemoryID = %d, want 12", request.MemoryID)
	}
	if request.Via != "huginn_x" {
		t.Fatalf("Via = %q, want huginn_x", request.Via)
	}
	if request.URL != "" {
		t.Fatalf("URL = %q, want empty for native publish", request.URL)
	}
}

func TestShellMemoryPublishViaHuginnXMarksApprovedOutcomePublished(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	driverPath := filepath.Join(t.TempDir(), "huginn-x-post-publish-driver.sh")
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := `#!/usr/bin/env bash
set -euo pipefail
request_path="$(mktemp)"
if [[ -n "${ODIN_TEST_X_PUBLISH_REQUEST_PATH:-}" ]]; then
	request_path="${ODIN_TEST_X_PUBLISH_REQUEST_PATH}"
fi
cat >"$request_path"
python3 - "$request_path" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    request = json.load(handle)
post_text = (((request.get("input") or {}).get("post_text")) or "").strip()
print(json.dumps({
    "status": "completed",
    "tool_key": "browser_x_post_publish",
    "summary": "Published approved X post through Browser Control.",
    "artifacts": {
        "publish_url": "https://x.com/marcus/status/999999999",
        "final_url": "https://x.com/marcus/status/999999999",
        "published_at": "2026-04-20T12:34:56Z",
        "screenshot_path": "/tmp/marcus-native-post.png",
        "posted_text": post_text
    }
}))
PY
`
	if err := os.WriteFile(driverPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", driverPath)
	t.Setenv("ODIN_TEST_X_PUBLISH_REQUEST_PATH", requestPath)

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_outcome result=approved channel=x content_kind=post -- Approved X post ready to publish natively.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}

	command := "/memory publish " + strconv.FormatInt(outcomes[0].ID, 10) + " via=huginn_x"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", command, err)
	}
	for _, want := range []string{
		"status=published",
		"publish_mode=huginn_x",
		"publish_url=https://x.com/marcus/status/999999999",
		"published_at=2026-04-20T12:34:56Z",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(requestPath) error = %v", err)
	}
	if !strings.Contains(string(requestBytes), `"tool_key":"browser_x_post_publish"`) {
		t.Fatalf("request = %q, want canonical browser_x_post_publish tool_key", string(requestBytes))
	}

	outcomes, err = env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(updated outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("updated outcome len = %d, want 1", len(outcomes))
	}
	details, err := parseMemoryDetails(outcomes[0].DetailsJSON)
	if err != nil {
		t.Fatalf("parseMemoryDetails(outcome) error = %v", err)
	}
	for key, want := range map[string]string{
		"publish_status":          "published",
		"publish_mode":            "huginn_x",
		"publish_url":             "https://x.com/marcus/status/999999999",
		"published_at":            "2026-04-20T12:34:56Z",
		"publish_screenshot_path": "/tmp/marcus-native-post.png",
	} {
		if got := details.Fields[key]; got != want {
			t.Fatalf("field %s = %q, want %q", key, got, want)
		}
	}
}

func TestShellMemoryPublishViaHuginnXMarksApprovedReplyPublished(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	driverPath := filepath.Join(t.TempDir(), "huginn-x-post-publish-driver.sh")
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := `#!/usr/bin/env bash
set -euo pipefail
request_path="${ODIN_TEST_X_PUBLISH_REQUEST_PATH}"
cat >"$request_path"
python3 - "$request_path" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    request = json.load(handle)
post_text = (((request.get("input") or {}).get("post_text")) or "").strip()
target_url = (((request.get("input") or {}).get("in_reply_to_url")) or "").strip()
print(json.dumps({
    "status": "completed",
    "tool_key": "browser_x_post_publish",
    "summary": "Published approved X reply through Browser Control.",
    "artifacts": {
        "publish_url": "https://x.com/marcus/status/888888888",
        "final_url": "https://x.com/marcus/status/888888888",
        "published_at": "2026-04-20T12:34:56Z",
        "screenshot_path": "/tmp/marcus-native-reply.png",
        "posted_text": post_text,
        "in_reply_to_url": target_url
    }
}))
PY
`
	if err := os.WriteFile(driverPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", driverPath)
	t.Setenv("ODIN_TEST_X_PUBLISH_REQUEST_PATH", requestPath)

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_outcome result=approved channel=x content_kind=reply in_reply_to_url=https://x.com/example/status/123 -- Short, useful reply text.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}

	command := "/memory publish " + strconv.FormatInt(outcomes[0].ID, 10) + " via=huginn_x"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", command, err)
	}
	for _, want := range []string{
		"status=published",
		"publish_mode=huginn_x",
		"publish_url=https://x.com/marcus/status/888888888",
		"published_at=2026-04-20T12:34:56Z",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(requestPath) error = %v", err)
	}
	if !strings.Contains(string(requestBytes), `"content_kind":"reply"`) {
		t.Fatalf("request = %q, want reply content_kind", string(requestBytes))
	}
	if !strings.Contains(string(requestBytes), `"tool_key":"browser_x_post_publish"`) {
		t.Fatalf("request = %q, want canonical browser_x_post_publish tool_key", string(requestBytes))
	}
	if !strings.Contains(string(requestBytes), `"in_reply_to_url":"https://x.com/example/status/123"`) {
		t.Fatalf("request = %q, want in_reply_to_url", string(requestBytes))
	}

	outcomes, err = env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(updated outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("updated outcome len = %d, want 1", len(outcomes))
	}
	details, err := parseMemoryDetails(outcomes[0].DetailsJSON)
	if err != nil {
		t.Fatalf("parseMemoryDetails(outcome) error = %v", err)
	}
	for key, want := range map[string]string{
		"publish_status":          "published",
		"publish_mode":            "huginn_x",
		"publish_url":             "https://x.com/marcus/status/888888888",
		"published_at":            "2026-04-20T12:34:56Z",
		"publish_screenshot_path": "/tmp/marcus-native-reply.png",
		"in_reply_to_url":         "https://x.com/example/status/123",
	} {
		if got := details.Fields[key]; got != want {
			t.Fatalf("field %s = %q, want %q", key, got, want)
		}
	}
}

func TestPublishApprovedXOutcomeWithHuginnStripsApprovalChecklistFromPostText(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	driverPath := filepath.Join(t.TempDir(), "huginn-x-post-publish-driver.sh")
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := `#!/usr/bin/env bash
set -euo pipefail
request_path="${ODIN_TEST_X_PUBLISH_REQUEST_PATH}"
cat >"$request_path"
python3 - "$request_path" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    request = json.load(handle)
post_text = (((request.get("input") or {}).get("post_text")) or "").strip()
print(json.dumps({
    "status": "completed",
    "tool_key": "browser_x_post_publish",
    "summary": "Published approved X post through Browser Control.",
    "artifacts": {
        "publish_url": "https://x.com/marcus/status/999999999",
        "final_url": "https://x.com/marcus/status/999999999",
        "published_at": "2026-04-20T12:34:56Z",
        "screenshot_path": "/tmp/marcus-native-post.png",
        "posted_text": post_text
    }
}))
PY
rm -f "$request_path"
`
	if err := os.WriteFile(driverPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", driverPath)
	t.Setenv("ODIN_TEST_X_PUBLISH_REQUEST_PATH", requestPath)

	detailsJSON := `{"source":"cli","selected_workflow_key":"marcus-social-growth-workflow","scope":"workflow","scope_key":"marcus-social-growth-workflow","fields":{"result":"approved","channel":"x","content_kind":"post"}}`
	recorded, err := env.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       "workflow",
		ScopeKey:    "marcus-social-growth-workflow",
		MemoryType:  "social_outcome",
		Summary:     "If the debrief starts at the flare, you're already late.\n\nApproval checklist:\n- Matches your normal instructor voice\n- No factual changes needed before posting",
		DetailsJSON: detailsJSON,
	})
	if err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}

	artifacts, err := shell.publishApprovedXOutcomeWithHuginn(ctx, recorded)
	if err != nil {
		t.Fatalf("publishApprovedXOutcomeWithHuginn() error = %v", err)
	}

	if got, want := strings.TrimSpace(stringMapValue(artifacts, "posted_text")), "If the debrief starts at the flare, you're already late."; got != want {
		t.Fatalf("posted_text = %q, want %q", got, want)
	}
}

func TestShellMemoryPublishViaHuginnXRejectsLinkedInOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_outcome result=approved channel=linkedin content_kind=post -- Approved LinkedIn post.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}

	command := "/memory publish " + strconv.FormatInt(outcomes[0].ID, 10) + " via=huginn_x"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", command, err)
	}
	if !strings.Contains(output.String(), "native X publish requires channel=x and content_kind=post or reply") {
		t.Fatalf("output = %q, want native X publish validation", output.String())
	}
}

func TestShellMemoryPublishViaHuginnXRejectsReplyWithoutTargetURL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_outcome result=approved channel=x content_kind=reply -- Short, useful reply text.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}

	command := "/memory publish " + strconv.FormatInt(outcomes[0].ID, 10) + " via=huginn_x"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", command, err)
	}
	if !strings.Contains(output.String(), "native X reply publish requires in_reply_to_url") {
		t.Fatalf("output = %q, want missing in_reply_to_url validation", output.String())
	}
}

func TestShellMemoryPublishViaHuginnXRejectsReplyWithInvalidTargetURL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_outcome result=approved channel=x content_kind=reply in_reply_to_url=https://example.com/not-x/123 -- Short, useful reply text.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}

	command := "/memory publish " + strconv.FormatInt(outcomes[0].ID, 10) + " via=huginn_x"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", command, err)
	}
	if !strings.Contains(output.String(), "native X reply publish requires in_reply_to_url to be a valid X status URL") {
		t.Fatalf("output = %q, want invalid in_reply_to_url validation", output.String())
	}
}

func TestShellMemoryPublishRejectsRejectedOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_outcome result=rejected channel=x content_kind=post -- Rejected X post.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}

	command := "/memory publish " + strconv.FormatInt(outcomes[0].ID, 10) + " url=https://x.com/marcus/status/123456789"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", command, err)
	}
	if !strings.Contains(output.String(), "only approved social_outcome memories can be published") {
		t.Fatalf("output = %q, want approved-only validation", output.String())
	}
}

func TestShellMemoryPublishRejectsAlreadyPublishedOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	for _, command := range []string{
		"/workflow use marcus-social-growth-workflow",
		"/memory remember social_outcome result=approved channel=linkedin content_kind=post publish_status=published publish_url=https://linkedin.com/feed/update/abc published_at=2026-04-20T00:00:00Z -- Already published LinkedIn post.",
	} {
		if err := shell.HandleLine(ctx, command, &output); err != nil {
			t.Fatalf("HandleLine(%s) error = %v", command, err)
		}
		output.Reset()
	}

	outcomes, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(outcomes) error = %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome len = %d, want 1", len(outcomes))
	}

	command := "/memory publish " + strconv.FormatInt(outcomes[0].ID, 10) + " url=https://linkedin.com/feed/update/def"
	if err := shell.HandleLine(ctx, command, &output); err != nil {
		t.Fatalf("HandleLine(%s) error = %v", command, err)
	}
	if !strings.Contains(output.String(), "social_outcome is already marked published") {
		t.Fatalf("output = %q, want already-published validation", output.String())
	}
}

func TestShellToolListHidesLegacyBrowserAliases(t *testing.T) {
	ctx := context.Background()
	env := newToolCommandTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/tool list", &output); err != nil {
		t.Fatalf("HandleLine(/tool list) error = %v", err)
	}
	if strings.Contains(output.String(), "huginn_visual_audit ") {
		t.Fatalf("output = %q, want hidden legacy alias omitted", output.String())
	}
	if !strings.Contains(output.String(), "browser_visual_audit ") {
		t.Fatalf("output = %q, want canonical browser tool listed", output.String())
	}
}

func TestShellToolShowLegacyBrowserAliasRendersCanonicalDefinition(t *testing.T) {
	ctx := context.Background()
	env := newToolCommandTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/tool show huginn_visual_audit", &output); err != nil {
		t.Fatalf("HandleLine(/tool show huginn_visual_audit) error = %v", err)
	}
	if !strings.Contains(output.String(), "tool=browser_visual_audit ") {
		t.Fatalf("output = %q, want canonical tool detail", output.String())
	}
	if strings.Contains(output.String(), "tool=huginn_visual_audit ") {
		t.Fatalf("output = %q, want legacy alias hidden in detail", output.String())
	}
}

func TestShellToolRunInvokesLiveVisualAuditTool(t *testing.T) {
	ctx := context.Background()
	env := newToolCommandTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "visual-driver.sh")
	script := `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"browser_visual_audit","summary":"Captured browser visual audit evidence for cfipros-dashboard.","artifacts":{"target_url":"https://example.com/dashboard","final_url":"https://example.com/dashboard","title":"Dashboard","label":"cfipros-dashboard","screenshot_path":"/tmp/dashboard.png","snapshot_excerpt":"Revenue MRR Pipeline","wait_ms":"2000","launch_mode":"--headless"}}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_HUGINN_VISUAL_DRIVER", scriptPath)

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/project alpha", &output); err != nil {
		t.Fatalf("HandleLine(/project alpha) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/tool run huginn_visual_audit target_url=https://example.com/dashboard label=cfipros-dashboard", &output); err != nil {
		t.Fatalf("HandleLine(/tool run) error = %v", err)
	}
	if !strings.Contains(output.String(), "tool=browser_visual_audit") {
		t.Fatalf("output = %q, want canonical browser tool result", output.String())
	}
	if !strings.Contains(output.String(), "artifact screenshot_path=/tmp/dashboard.png") {
		t.Fatalf("output = %q, want screenshot artifact", output.String())
	}
}

func TestShellToolRunInvokesLiveVisualAuditToolInGlobalScope(t *testing.T) {
	ctx := context.Background()
	env := newToolCommandTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "visual-driver.sh")
	script := `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"browser_visual_audit","summary":"Captured browser visual audit evidence for x-compose.","artifacts":{"target_url":"https://x.com/compose/post","final_url":"https://x.com/compose/post","title":"X","label":"x-compose","screenshot_path":"/tmp/x-compose.png","snapshot_excerpt":"What is happening?!","wait_ms":"2000","launch_mode":"--headed"}}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_HUGINN_VISUAL_DRIVER", scriptPath)

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/tool run huginn_visual_audit target_url=https://x.com/compose/post label=x-compose headless=false", &output); err != nil {
		t.Fatalf("HandleLine(/tool run global visual audit) error = %v", err)
	}
	if !strings.Contains(output.String(), "tool=browser_visual_audit") {
		t.Fatalf("output = %q, want canonical browser tool result", output.String())
	}
	if !strings.Contains(output.String(), "artifact final_url=https://x.com/compose/post") {
		t.Fatalf("output = %q, want final_url artifact", output.String())
	}
	if !strings.Contains(output.String(), "artifact screenshot_path=/tmp/x-compose.png") {
		t.Fatalf("output = %q, want screenshot artifact", output.String())
	}
}

func TestShellToolRunRecordsWorkflowScopedXVisibleEvidence(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "x-post-driver.sh")
	script := `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"browser_x_post_visible_evidence","summary":"Captured visible X post evidence for marcus-crosswind.","artifacts":{"target_url":"https://x.com/marcus/status/123","final_url":"https://x.com/marcus/status/123","label":"marcus-crosswind","title":"X","screenshot_path":"/tmp/marcus-crosswind.png","snapshot_path":"/tmp/marcus-crosswind.txt","snapshot_excerpt":"Students do not need more motivation","post_text":"Students do not need more motivation","author_display_name":"Marcus Gollahon","author_handle":"@marcus","reply_count":"4","repost_count":"2","like_count":"18","bookmark_count":"1","view_count":"1400"}}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", scriptPath)

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output); err != nil {
		t.Fatalf("HandleLine(/workflow use) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/tool run browser_x_post_visible_evidence target_url=https://x.com/marcus/status/123 label=marcus-crosswind", &output); err != nil {
		t.Fatalf("HandleLine(/tool run x evidence) error = %v", err)
	}
	if !strings.Contains(output.String(), "tool=browser_x_post_visible_evidence") {
		t.Fatalf("output = %q, want canonical browser tool result", output.String())
	}
	if !strings.Contains(output.String(), "artifact snapshot_path=/tmp/marcus-crosswind.txt") {
		t.Fatalf("output = %q, want snapshot artifact", output.String())
	}
	if !strings.Contains(output.String(), "fact bookmark_count=1") {
		t.Fatalf("output = %q, want bookmark_count fact", output.String())
	}
	if !strings.Contains(output.String(), "tool_memory=") {
		t.Fatalf("output = %q, want recorded tool memory", output.String())
	}

	summaries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_evidence",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(social_evidence) error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("social evidence len = %d, want 1", len(summaries))
	}
	details, err := parseMemoryDetails(summaries[0].DetailsJSON)
	if err != nil {
		t.Fatalf("parseMemoryDetails(social_evidence) error = %v", err)
	}
	if details.Fields["channel"] != "x" {
		t.Fatalf("channel = %q, want x", details.Fields["channel"])
	}
	if details.Fields["evidence_kind"] != "x_post_visible" {
		t.Fatalf("evidence_kind = %q, want x_post_visible", details.Fields["evidence_kind"])
	}
	if details.Fields["target_url"] != "https://x.com/marcus/status/123" {
		t.Fatalf("target_url = %q, want source URL", details.Fields["target_url"])
	}
	if details.Fields["bookmark_count"] != "1" {
		t.Fatalf("bookmark_count = %q, want 1", details.Fields["bookmark_count"])
	}
}

func TestShellToolRunLegacyXVisibleEvidenceAliasRendersCanonicalKey(t *testing.T) {
	ctx := context.Background()
	env := newToolCommandTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "x-post-driver.sh")
	script := `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"browser_x_post_visible_evidence","summary":"Captured visible X post evidence for marcus-crosswind.","artifacts":{"target_url":"https://x.com/marcus/status/123","final_url":"https://x.com/marcus/status/123","label":"marcus-crosswind","title":"X","screenshot_path":"/tmp/marcus-crosswind.png","snapshot_path":"/tmp/marcus-crosswind.txt","snapshot_excerpt":"Students do not need more motivation","post_text":"Students do not need more motivation","author_display_name":"Marcus Gollahon","author_handle":"@marcus","reply_count":"4","repost_count":"2","like_count":"18","bookmark_count":"1","view_count":"1400"}}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", scriptPath)

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/tool run huginn_x_post_visible_evidence target_url=https://x.com/marcus/status/123 label=marcus-crosswind", &output); err != nil {
		t.Fatalf("HandleLine(/tool run legacy x evidence) error = %v", err)
	}
	if !strings.Contains(output.String(), "tool=browser_x_post_visible_evidence") {
		t.Fatalf("output = %q, want canonical browser tool result", output.String())
	}
	if !strings.Contains(output.String(), "artifact snapshot_path=/tmp/marcus-crosswind.txt") {
		t.Fatalf("output = %q, want snapshot artifact", output.String())
	}
}

func TestShellToolRunRecordsWorkflowScopedXWeeklyEvidenceBundle(t *testing.T) {
	ctx := context.Background()
	env := newTestEnvironment(t)
	shell, err := New(env)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "x-weekly-driver.sh")
	script := `#!/usr/bin/env bash
request_path="$(mktemp)"
cat >"$request_path"
python3 - "$request_path" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    request = json.load(handle)
target_url = (((request.get("input") or {}).get("target_url")) or "").strip()
suffix = target_url.rsplit("/", 1)[-1] or "post"
print(json.dumps({
    "status": "completed",
    "tool_key": "browser_x_post_visible_evidence",
    "summary": f"Captured visible X post evidence for weekly-review-{suffix}.",
    "artifacts": {
        "target_url": target_url,
        "final_url": target_url,
        "label": f"weekly-review-{suffix}",
        "title": "X",
        "screenshot_path": f"/tmp/{suffix}.png",
        "snapshot_path": f"/tmp/{suffix}.txt",
        "snapshot_excerpt": f"Excerpt for {suffix}",
        "post_text": f"Post text for {suffix}",
        "author_display_name": "Marcus Gollahon",
        "author_handle": "@marcus",
        "reply_count": "4",
        "repost_count": "2",
        "like_count": "18",
        "view_count": "1400"
    }
}))
PY
rm -f "$request_path"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", scriptPath)

	var output bytes.Buffer
	if err := shell.HandleLine(ctx, "/workflow use marcus-social-growth-workflow", &output); err != nil {
		t.Fatalf("HandleLine(/workflow use) error = %v", err)
	}
	output.Reset()
	if err := shell.HandleLine(ctx, "/tool run browser_x_weekly_evidence_bundle target_urls=https://x.com/marcus/status/123,https://x.com/marcus/status/456 label=weekly-review", &output); err != nil {
		t.Fatalf("HandleLine(/tool run x weekly bundle) error = %v", err)
	}
	if !strings.Contains(output.String(), "tool=browser_x_weekly_evidence_bundle") {
		t.Fatalf("output = %q, want canonical weekly bundle tool result", output.String())
	}
	if !strings.Contains(output.String(), "fact recorded_evidence=2") {
		t.Fatalf("output = %q, want recorded evidence count", output.String())
	}
	if strings.Count(output.String(), "tool_memory=") != 2 {
		t.Fatalf("output = %q, want two recorded tool memory entries", output.String())
	}

	summaries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_evidence",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(social_evidence) error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("social evidence len = %d, want 2", len(summaries))
	}
	first, err := parseMemoryDetails(summaries[0].DetailsJSON)
	if err != nil {
		t.Fatalf("parseMemoryDetails(first social_evidence) error = %v", err)
	}
	second, err := parseMemoryDetails(summaries[1].DetailsJSON)
	if err != nil {
		t.Fatalf("parseMemoryDetails(second social_evidence) error = %v", err)
	}
	if first.Fields["bundle_label"] != "weekly-review" || second.Fields["bundle_label"] != "weekly-review" {
		t.Fatalf("bundle labels = %q and %q, want weekly-review", first.Fields["bundle_label"], second.Fields["bundle_label"])
	}
	if first.Fields["bundle_position"] == second.Fields["bundle_position"] {
		t.Fatalf("bundle positions = %q and %q, want distinct ordering", first.Fields["bundle_position"], second.Fields["bundle_position"])
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
		Store:            store,
		Registry:         registry,
		RegistrySnapshot: testRegistrySnapshot(),
		SessionStore:     SessionStore{Path: filepath.Join(stateDir, "cli-session.json")},
		ExecutorConfig:   mustLoadExecutorConfig(t),
		Executors:        executorrouter.DefaultCatalog(),
		Leases: leases.Manager{
			Store:        store,
			Git:          shellTestGit{},
			WorktreeRoot: t.TempDir(),
		},
	}
}

func newToolCommandTestEnvironment(t *testing.T) Environment {
	t.Helper()

	baseDir := t.TempDir()
	stateDir := filepath.Join(baseDir, "state")
	dataDir := filepath.Join(baseDir, "data")
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
		Store:            store,
		Registry:         registry,
		RegistrySnapshot: testRegistrySnapshot(),
		SessionStore:     SessionStore{Path: filepath.Join(stateDir, "cli-session.json")},
		ExecutorConfig:   executorrouter.Config{},
		Executors:        map[string]contract.Executor{},
		Leases: leases.Manager{
			Store:        store,
			Git:          shellTestGit{},
			WorktreeRoot: t.TempDir(),
		},
	}
}

func recordWorkflowMemoryAtTime(t *testing.T, ctx context.Context, env Environment, workflowKey string, memoryType string, summary string, fields map[string]string, createdAt time.Time) sqlite.MemorySummary {
	t.Helper()

	details, err := json.Marshal(map[string]any{
		"source":                "test",
		"selected_workflow_key": workflowKey,
		"scope":                 "workflow",
		"scope_key":             workflowKey,
		"fields":                fields,
	})
	if err != nil {
		t.Fatalf("json.Marshal(details) error = %v", err)
	}

	recorded, err := env.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       "workflow",
		ScopeKey:    workflowKey,
		MemoryType:  memoryType,
		Summary:     summary,
		DetailsJSON: string(details),
	})
	if err != nil {
		t.Fatalf("RecordMemorySummary() error = %v", err)
	}

	timestamp := createdAt.UTC().Format(time.RFC3339Nano)
	if _, err := env.Store.DB().ExecContext(ctx, `
		UPDATE memory_summaries
		SET created_at = ?, updated_at = ?
		WHERE id = ?
	`, timestamp, timestamp, recorded.ID); err != nil {
		t.Fatalf("updating memory summary timestamps error = %v", err)
	}

	updatedEntries, err := env.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   workflowKey,
		MemoryType: memoryType,
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	for _, entry := range updatedEntries {
		if entry.ID == recorded.ID {
			return entry
		}
	}
	t.Fatalf("updated memory summary %d not found", recorded.ID)
	return sqlite.MemorySummary{}
}

func testRegistrySnapshot() registry.Snapshot {
	items := []registry.Item{
		{
			Kind:       registry.KindSkill,
			Key:        "pixel-perfect-ui-ux-designer",
			Title:      "Pixel Perfect UI/UX Designer",
			Summary:    "Audit product UI with strong taste and visual verification.",
			Tags:       []string{"design", "ux"},
			Scopes:     []string{"project", "odin-core"},
			Strictness: "adaptive",
			AppliesTo:  []string{"design_audit", "visual_improvement"},
			Sections: map[string]string{
				registry.SectionPurpose:         "Audit the live interface with project-specific design taste.",
				registry.SectionWhenToUse:       "Use for dashboards and product UI reviews.",
				registry.SectionInputs:          "Screenshots, live pages, product context, and existing UI.",
				registry.SectionProcedure:       "Inspect the interface, identify hierarchy problems, use visual evidence, and propose concrete improvements.",
				registry.SectionOutputs:         "Produce a specific audit, a stronger direction, and implementation guidance.",
				registry.SectionConstraints:     "Avoid generic AI dashboard patterns and unsupported visual claims.",
				registry.SectionSuccessCriteria: "Recommendations are concrete, project-specific, and visually justified.",
			},
			Source: registry.SourceInfo{RelativePath: "skills/pixel-perfect-ui-ux-designer.md"},
		},
		{
			Kind:       registry.KindSkill,
			Key:        "marcus-social-analytics-advisor",
			Title:      "Analytics and Retrospective Advisor",
			Summary:    "Reviews recent social performance and approval patterns to improve Marcus's next content cycle.",
			Tags:       []string{"social", "analytics", "retrospective"},
			Scopes:     []string{"global"},
			Strictness: "rigid",
			AppliesTo:  []string{"analytics", "retrospective", "planning-feedback"},
			Sections: map[string]string{
				registry.SectionPurpose:         "Turn recent content output and performance data into practical learnings for Marcus's next week.",
				registry.SectionWhenToUse:       "Use after a publishing cycle or whenever Marcus wants a clear retrospective.",
				registry.SectionInputs:          "Recent post history, approval outcomes, engagement notes, and qualitative observations.",
				registry.SectionProcedure:       "Review the recent cycle, compare performance by topic and structure, and recommend concrete next-step adjustments.",
				registry.SectionOutputs:         "A short retrospective, metric summary, approval pattern review, and next-week recommendations.",
				registry.SectionConstraints:     "Do not optimize blindly for vanity metrics and do not claim precision the data does not support.",
				registry.SectionSuccessCriteria: "The next planning cycle is stronger because the real signal was kept and the noise discarded.",
			},
			Source: registry.SourceInfo{RelativePath: "skills/marcus-social-analytics-advisor.md"},
		},
		{
			Kind:       registry.KindSkill,
			Key:        "marcus-x-drafting-assistant",
			Title:      "X Drafting Assistant",
			Summary:    "Turns ideas into concise X posts or short thread seeds with Marcus's voice.",
			Tags:       []string{"social", "x", "drafting"},
			Scopes:     []string{"global"},
			Strictness: "rigid",
			AppliesTo:  []string{"drafting", "x"},
			Sections: map[string]string{
				registry.SectionPurpose:         "Convert one aviation insight into an X draft that is sharp, readable, human, and worth Marcus's approval without over-polishing it.",
				registry.SectionWhenToUse:       "Use when an idea should become an X post or short thread seed.",
				registry.SectionInputs:          "The skill expects the topic, intended audience, real lesson or opinion, any required facts, and any style constraints Marcus wants honored.",
				registry.SectionProcedure:       "Find the core point, write a strong opening, keep the language tight, remove fluff, avoid over-editing into polished-perfect prose, and make any factual or sensitivity checks explicit.",
				registry.SectionOutputs:         "The output is one primary X draft, optional alternates, and a short approval checklist.",
				registry.SectionConstraints:     "Do not use engagement bait, fake hustle language, spammy duplication, or unsupported factual claims. Keep it text-only and human-reviewable. Do not optimize for perfect grammar if that makes the draft sound less like a real X post from Marcus.",
				registry.SectionSuccessCriteria: "Marcus can approve or revise the draft quickly because it is concise, useful, and on-brand.",
			},
			Source: registry.SourceInfo{RelativePath: "skills/marcus-x-drafting-assistant.md"},
		},
		{
			Kind:       registry.KindSkill,
			Key:        "marcus-engagement-research-assistant",
			Title:      "Engagement Research Assistant",
			Summary:    "Suggests compliant reply opportunities and response drafts while filtering out low-value or high-risk conversations.",
			Tags:       []string{"social", "replies", "research"},
			Scopes:     []string{"global"},
			Strictness: "rigid",
			AppliesTo:  []string{"engagement-research", "reply-suggestions", "risk-screening"},
			Sections: map[string]string{
				registry.SectionPurpose:         "Help Marcus engage selectively by identifying worthwhile reply opportunities and drafting thoughtful responses only when they add value.",
				registry.SectionWhenToUse:       "Use when Marcus wants suggested replies, a screen for whether to engage, or help evaluating public posts and conversations.",
				registry.SectionInputs:          "The skill expects candidate post URLs or conversation summaries, current context, any sensitivity flags, and Marcus's desired level of engagement. When Marcus wants a reply that can stay on the canonical Odin publish path later, include the explicit target X post URL.",
				registry.SectionProcedure:       "Classify each candidate as reply, monitor, or skip, explain the reasoning, draft responses only for the strongest opportunities, preserve the explicit target post URL when one is provided, and flag anything sensitive for explicit approval.",
				registry.SectionOutputs:         "A ranked engagement list, reply suggestions, skip recommendations, and sensitivity notes.",
				registry.SectionConstraints:     "Do not recommend automated engagement, argumentative bait, or replies on topics that need caution unless the approval warning is explicit. The default should favor restraint over noise. When drafting X replies, do not optimize for perfect grammar or polished sentences if the response is already clear, useful, and human.",
				registry.SectionSuccessCriteria: "Marcus gets fewer, better engagement options and avoids wasting time or taking unnecessary reputational risk.",
			},
			Source: registry.SourceInfo{RelativePath: "skills/marcus-engagement-research-assistant.md"},
		},
		{
			Kind:    registry.KindAgent,
			Key:     "marcus-social-content-strategist-companion",
			Title:   "Content Strategist Advisor",
			Summary: "Plans Marcus's aviation authority growth across X and LinkedIn.",
			Tags:    []string{"social", "aviation", "strategy"},
			Scopes:  []string{"global"},
			Tools:   []string{"filesystem", "web"},
			Role:    "social-content-strategist",
			Sections: map[string]string{
				registry.SectionPurpose:         "Provide the durable strategy role for Marcus's social initiative.",
				registry.SectionWhenToUse:       "Use when Odin needs a weekly plan or topic prioritization.",
				registry.SectionInputs:          "Topic pillars, content history, performance notes, and platform constraints.",
				registry.SectionProcedure:       "Review what Marcus has already said, pick the strongest angles, and build a realistic weekly plan.",
				registry.SectionOutputs:         "A short weekly plan, platform mapping, and any approval-sensitive topics.",
				registry.SectionConstraints:     "Do not recommend stealth automation, fake controversy, or publishing without review.",
				registry.SectionSuccessCriteria: "Marcus gets a realistic, on-brand content plan.",
			},
			Source: registry.SourceInfo{RelativePath: "agents/marcus-social-content-strategist-companion.md"},
		},
		{
			Kind:    registry.KindAgent,
			Key:     "portal-delivery-agent",
			Title:   "Portal Delivery Agent",
			Summary: "Coordinates child delivery work for a portal surface.",
			Tags:    []string{"portal", "delivery"},
			Scopes:  []string{"project", "odin-core"},
			Tools:   []string{"filesystem"},
			Role:    "portal-delivery",
			Sections: map[string]string{
				registry.SectionPurpose:         "Coordinate child work needed to deliver a portal surface.",
				registry.SectionWhenToUse:       "Use when a portal track should be delivered through Odin child tasks.",
				registry.SectionInputs:          "Portal track, surface, goal, and project scope.",
				registry.SectionProcedure:       "Create child work for IA, design, implementation, visual verification, and learning capture.",
				registry.SectionOutputs:         "A parent run, child delegations, and auditable delivery evidence.",
				registry.SectionConstraints:     "Do not bypass project work outside Odin task execution.",
				registry.SectionSuccessCriteria: "Portal delivery evidence is visible through runs, memory, and delegation artifacts.",
			},
			Source: registry.SourceInfo{RelativePath: "agents/portal-delivery-agent.md"},
		},
		{
			Kind:       registry.KindWorkflow,
			Key:        "marcus-social-growth-workflow",
			Title:      "Marcus Social Growth Workflow",
			Summary:    "Coordinates compliant planning, drafting, review, approval, publishing, and retrospective work.",
			Tags:       []string{"social", "workflow"},
			Entrypoint: "skill:marcus-social-content-strategist",
			Composes: []string{
				"marcus-social-content-strategist",
				"marcus-x-drafting-assistant",
				"marcus-linkedin-drafting-assistant",
				"marcus-engagement-research-assistant",
				"marcus-social-analytics-advisor",
			},
			Sections: map[string]string{
				registry.SectionPurpose:         "Define the shared workflow for compliant text-first social operations.",
				registry.SectionWhenToUse:       "Use when Marcus wants to plan content, draft posts, or review outcomes.",
				registry.SectionInputs:          "Topic ideas, platform goals, voice preferences, and content history.",
				registry.SectionProcedure:       "Plan the week, classify ideas, draft, review, approve, publish manually, and record learnings.",
				registry.SectionOutputs:         "A weekly plan, draft set, approval notes, and a retrospective summary.",
				registry.SectionConstraints:     "Do not automate publishing through deceptive means or bypass approval gates.",
				registry.SectionSuccessCriteria: "Marcus can use Odin as a reliable social copilot without crossing compliance boundaries.",
			},
			Source: registry.SourceInfo{RelativePath: "workflows/marcus-social-growth-workflow.md"},
		},
	}

	byKey := make(map[string]registry.Item, len(items))
	byKind := make(map[registry.Kind][]registry.Item)
	for _, item := range items {
		byKey[item.Key] = item
		byKind[item.Kind] = append(byKind[item.Kind], item)
	}

	return registry.Snapshot{
		Items:  items,
		ByKey:  byKey,
		ByKind: byKind,
	}
}

type shellTestGit struct{}

func (shellTestGit) BranchExists(context.Context, string, string) (bool, error) { return false, nil }
func (shellTestGit) CreateBranch(context.Context, string, string, string) error { return nil }
func (shellTestGit) AddWorktree(context.Context, string, string, string) error  { return nil }
func (shellTestGit) RemoveWorktree(context.Context, string, string) error       { return nil }

func mustLoadExecutorConfig(t *testing.T) executorrouter.Config {
	t.Helper()

	cfg, err := executorrouter.LoadConfig(filepath.Clean(filepath.Join("..", "..", "..", "config", "executors.yaml")))
	if err != nil {
		t.Fatalf("LoadConfig(executors) error = %v", err)
	}
	return cfg
}

func hasTransitionEvent(events []runtimeevents.Record, want runtimeevents.Type) bool {
	for _, event := range events {
		if event.Type == want {
			return true
		}
	}
	return false
}

func registryConfigPath(t *testing.T, registry projects.Registry) string {
	t.Helper()

	if systemProject, ok := registry.SystemProject(); ok && systemProject.SourcePath != "" {
		return systemProject.SourcePath
	}
	projectList := registry.Projects()
	if len(projectList) == 0 || projectList[0].SourcePath == "" {
		t.Fatalf("registry does not expose a config path")
	}
	return projectList[0].SourcePath
}
