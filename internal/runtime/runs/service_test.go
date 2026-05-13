package runs

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	"odin-os/internal/store/sqlite"
)

func TestListFiltersRunsByScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	alphaProject, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
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
	coreProject, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
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

	alphaTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   alphaProject.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha) error = %v", err)
	}
	coreTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
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

	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   alphaTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun(alpha) error = %v", err)
	}
	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   coreTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun(core) error = %v", err)
	}

	service := Service{
		DB: store.DB(),
	}

	views, err := service.List(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(views) != 1 || views[0].TaskKey != "alpha-task" {
		t.Fatalf("views = %+v, want one alpha run", views)
	}
}

func TestGetRunReturnsRunRecord(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
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

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	service := Service{DB: store.DB()}
	record, err := service.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if record.RunID != run.ID {
		t.Fatalf("GetRun().RunID = %d, want %d", record.RunID, run.ID)
	}
	if record.Status != "running" {
		t.Fatalf("GetRun().Status = %q, want %q", record.Status, "running")
	}
	if record.FinishedAt != nil {
		t.Fatalf("GetRun().FinishedAt = %v, want nil", record.FinishedAt)
	}
}

func TestShowReturnsRunDetailWithinScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
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

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	service := Service{DB: store.DB()}
	detail, err := service.Show(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, run.ID)
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if detail.RunID != run.ID {
		t.Fatalf("Show().RunID = %d, want %d", detail.RunID, run.ID)
	}
	if detail.TaskKey != task.Key {
		t.Fatalf("Show().TaskKey = %q, want %q", detail.TaskKey, task.Key)
	}
	if detail.Executor != "codex" {
		t.Fatalf("Show().Executor = %q, want %q", detail.Executor, "codex")
	}
}

func TestRunsDetailIncludesRunArtifacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
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

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	if _, err := store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        run.ID,
		ArtifactType: "driver_result",
		Summary:      "Robinhood review ready",
		DetailsJSON:  `{"session_state":"review_ready"}`,
	}); err != nil {
		t.Fatalf("RecordRunArtifact() error = %v", err)
	}

	service := Service{DB: store.DB(), Store: store}
	detail, err := service.Show(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, run.ID)
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}

	if len(detail.Artifacts) != 1 {
		t.Fatalf("Show().Artifacts len = %d, want 1", len(detail.Artifacts))
	}
	if detail.Artifacts[0].ArtifactType != "driver_result" {
		t.Fatalf("Show().Artifacts[0].ArtifactType = %q, want %q", detail.Artifacts[0].ArtifactType, "driver_result")
	}
	if detail.Artifacts[0].Summary != "Robinhood review ready" {
		t.Fatalf("Show().Artifacts[0].Summary = %q, want %q", detail.Artifacts[0].Summary, "Robinhood review ready")
	}
	if detail.Artifacts[0].DetailsJSON != `{"session_state":"review_ready"}` {
		t.Fatalf("Show().Artifacts[0].DetailsJSON = %q, want %q", detail.Artifacts[0].DetailsJSON, `{"session_state":"review_ready"}`)
	}
}

func TestShowDecodesFailureAnalysisArtifact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
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

	service := Service{DB: store.DB(), Store: store}
	detail, err := service.Show(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, run.ID)
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if detail.ArtifactsJSON != artifactJSON {
		t.Fatalf("Show().ArtifactsJSON = %q, want raw failure analysis artifact", detail.ArtifactsJSON)
	}
	if detail.FailureAnalysis == nil {
		t.Fatalf("Show().FailureAnalysis = nil, want decoded failure analysis")
	}
	if detail.FailureAnalysis.Category != "test_failure" ||
		detail.FailureAnalysis.SuggestedFix != "Inspect failing test output and repair the regression." ||
		detail.FailureAnalysis.NextStepTarget != "test" ||
		!detail.FailureAnalysis.RetryRecommended ||
		!detail.FailureAnalysis.FollowUpRecommended ||
		detail.FailureAnalysis.FollowUpTitle != "Fix flaky test" ||
		detail.FailureAnalysis.FollowUpReason != "needs a focused repair" {
		t.Fatalf("Show().FailureAnalysis = %+v, want decoded operator fields", detail.FailureAnalysis)
	}
}

func TestGetRunEnvelopeReturnsEmptyArtifactsByDefault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
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

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	service := Service{DB: store.DB()}
	envelope, err := service.GetRunEnvelope(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunEnvelope() error = %v", err)
	}
	if envelope.RunID != strconv.FormatInt(run.ID, 10) {
		t.Fatalf("GetRunEnvelope().RunID = %q, want %q", envelope.RunID, strconv.FormatInt(run.ID, 10))
	}
	if envelope.Status != "running" {
		t.Fatalf("GetRunEnvelope().Status = %q, want %q", envelope.Status, "running")
	}
	if len(envelope.Artifacts) != 0 {
		t.Fatalf("GetRunEnvelope().Artifacts = %+v, want empty", envelope.Artifacts)
	}
}

func TestServiceCompletesRunAndTaskWithTerminalState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunStore(t)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
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
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-run",
		Title:       "Alpha run",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha) error = %v", err)
	}

	service := Service{Store: store}
	run, err := service.Start(ctx, task, "fake_headless")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	result := contract.ExecutionResult{
		Status: "completed",
		Output: "run service summary",
		Metadata: map[string]string{
			"artifacts_json": `["runs/artifacts/run-service.json"]`,
		},
	}
	if err := service.Complete(ctx, run.ID, result); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Summary != "run service summary" {
		t.Fatalf("run summary = %q, want run service summary", gotRun.Summary)
	}
	if gotRun.TerminalReason != "completed" {
		t.Fatalf("run terminal reason = %q, want completed", gotRun.TerminalReason)
	}
	if gotRun.ArtifactsJSON != `["runs/artifacts/run-service.json"]` {
		t.Fatalf("run artifacts = %q, want persisted artifact pointer", gotRun.ArtifactsJSON)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "completed" {
		t.Fatalf("task status = %q, want completed", gotTask.Status)
	}
	if gotTask.Summary != "run service summary" {
		t.Fatalf("task summary = %q, want run service summary", gotTask.Summary)
	}
	if gotTask.TerminalReason != "completed" {
		t.Fatalf("task terminal reason = %q, want completed", gotTask.TerminalReason)
	}
	if gotTask.ArtifactsJSON != `["runs/artifacts/run-service.json"]` {
		t.Fatalf("task artifacts = %q, want persisted artifact pointer", gotTask.ArtifactsJSON)
	}
}

func openRunStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func writeRegistry(t *testing.T) projects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")

	for _, key := range []string{"odin-core", "alpha"} {
		gitRoot := filepath.Join(root, key)
		if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir git root: %v", err)
		}
	}

	if err := os.WriteFile(configPath, []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: odin-core
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
  - key: alpha
    name: Alpha
    project_class: github_backed_project
    git_root: alpha
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
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	registry, diagnostics, err := projects.Register(configPath)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Register() diagnostics = %#v", diagnostics)
	}

	return registry
}
