package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"odin-os/internal/core/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestLoadInitializesFreshRuntimeReadinessState(t *testing.T) {
	repoRoot := bootstrapTestRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	if len(app.RegistryDiagnostics) != 0 {
		t.Fatalf("RegistryDiagnostics = %+v, want none", app.RegistryDiagnostics)
	}

	assertCountAtLeast(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM registry_versions"), 1)
	assertCountAtLeast(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM executor_health"), 1)
	assertCountAtLeast(t, app.Store.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM projection_freshness"), 1)
}

func TestLoadBootstrapsDefaultWorkspaceAndCompanion(t *testing.T) {
	repoRoot := bootstrapTestRepoRoot(t)
	runtimeRoot := t.TempDir()

	app, err := Load(context.Background(), repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	workspace, err := app.Store.GetWorkspaceByKey(context.Background(), workspaces.DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	if workspace.DefaultCompanionKey != workspaces.DefaultWorkspaceCompanionKey {
		t.Fatalf("GetWorkspaceByKey(default).DefaultCompanionKey = %q, want %q", workspace.DefaultCompanionKey, workspaces.DefaultWorkspaceCompanionKey)
	}

	companion, err := app.Store.GetCompanionByKey(context.Background(), workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	if companion.Key != workspace.DefaultCompanionKey {
		t.Fatalf("GetCompanionByKey(default).Key = %q, want %q", companion.Key, workspace.DefaultCompanionKey)
	}
}

func TestLoadRepairsLegacyProjectsAndTasksIntoWorkspaceModel(t *testing.T) {
	ctx := context.Background()
	repoRoot := bootstrapTestRepoRoot(t)
	runtimeRoot := t.TempDir()

	if err := os.MkdirAll(filepath.Join(runtimeRoot, "data"), 0o755); err != nil {
		t.Fatalf("MkdirAll(data) error = %v", err)
	}

	seedStore, err := sqlite.Open(filepath.Join(runtimeRoot, "data", "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open(seed) error = %v", err)
	}
	if err := seedStore.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(seed) error = %v", err)
	}

	project, err := seedStore.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "legacy-project",
		Name:          "Legacy Project",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "legacy-project"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := seedStore.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "legacy-task",
		Title:       "Legacy task",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if err := seedStore.Close(); err != nil {
		t.Fatalf("seedStore.Close() error = %v", err)
	}

	app, err := Load(ctx, repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer app.Store.Close()

	workspace, err := app.Store.GetWorkspaceByKey(ctx, workspaces.DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	initiative, err := app.Store.GetInitiativeByKey(ctx, workspace.ID, project.Key)
	if err != nil {
		t.Fatalf("GetInitiativeByKey(legacy-project) error = %v", err)
	}
	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("GetInitiativeByKey(legacy-project).LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}

	repairedTask, err := app.Store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask(legacy-task) error = %v", err)
	}
	if repairedTask.WorkspaceID == nil || *repairedTask.WorkspaceID != workspace.ID {
		t.Fatalf("GetTask(legacy-task).WorkspaceID = %v, want %d", repairedTask.WorkspaceID, workspace.ID)
	}
	if repairedTask.InitiativeID == nil || *repairedTask.InitiativeID != initiative.ID {
		t.Fatalf("GetTask(legacy-task).InitiativeID = %v, want %d", repairedTask.InitiativeID, initiative.ID)
	}
	if repairedTask.WorkKind != repairedTask.Scope {
		t.Fatalf("GetTask(legacy-task).WorkKind = %q, want %q", repairedTask.WorkKind, repairedTask.Scope)
	}
}

func TestLoadSerializesConcurrentBootstrapForFreshRuntime(t *testing.T) {
	repoRoot := bootstrapTestRepoRoot(t)
	runtimeRoot := t.TempDir()

	var entered int32
	lockAcquired := make(chan struct{})
	release := make(chan struct{})
	testBootstrapHooks.afterLockAcquired = func() {
		if atomic.AddInt32(&entered, 1) != 1 {
			return
		}
		close(lockAcquired)
		<-release
	}
	t.Cleanup(func() {
		testBootstrapHooks.afterLockAcquired = nil
	})

	type result struct {
		app App
		err error
	}

	firstResult := make(chan result, 1)
	secondResult := make(chan result, 1)

	go func() {
		app, err := Load(context.Background(), repoRoot, runtimeRoot)
		firstResult <- result{app: app, err: err}
	}()

	<-lockAcquired

	go func() {
		app, err := Load(context.Background(), repoRoot, runtimeRoot)
		secondResult <- result{app: app, err: err}
	}()

	select {
	case result := <-secondResult:
		if result.app.Store != nil {
			_ = result.app.Store.Close()
		}
		t.Fatalf("second Load() completed before first bootstrap released the lock: err=%v", result.err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	first := <-firstResult
	if first.err != nil {
		t.Fatalf("first Load() error = %v", first.err)
	}
	defer first.app.Store.Close()

	second := <-secondResult
	if second.err != nil {
		t.Fatalf("second Load() error = %v", second.err)
	}
	defer second.app.Store.Close()
}

func TestLoadReturnsBootstrapTimeoutWhenLockWaitExceedsConfiguredLimit(t *testing.T) {
	repoRoot := bootstrapTestRepoRoot(t)
	runtimeRoot := t.TempDir()
	t.Setenv("ODIN_BOOTSTRAP_TIMEOUT", "50ms")

	lockAcquired := make(chan struct{})
	release := make(chan struct{})
	var entered int32
	testBootstrapHooks.afterLockAcquired = func() {
		if atomic.AddInt32(&entered, 1) != 1 {
			return
		}
		close(lockAcquired)
		<-release
	}
	t.Cleanup(func() {
		testBootstrapHooks.afterLockAcquired = nil
	})

	firstResult := make(chan error, 1)
	go func() {
		app, err := Load(context.Background(), repoRoot, runtimeRoot)
		if err == nil {
			_ = app.Store.Close()
		}
		firstResult <- err
	}()

	<-lockAcquired

	_, err := Load(context.Background(), repoRoot, runtimeRoot)
	close(release)

	if err == nil {
		t.Fatal("second Load() error = nil, want bootstrap timeout")
	}

	var timeoutErr *BootstrapTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("Load() error = %v, want BootstrapTimeoutError", err)
	}

	if firstErr := <-firstResult; firstErr != nil {
		t.Fatalf("first Load() error = %v", firstErr)
	}
}

func bootstrapTestRepoRoot(t *testing.T) string {
	t.Helper()

	sourceRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	root := t.TempDir()

	for _, dir := range []string{
		filepath.Join(root, "config"),
		filepath.Join(root, "registry"),
		filepath.Join(root, "odin-core", ".git"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}

	for _, name := range []string{"odin.yaml", "executors.yaml", "models.yaml", "telemetry.yaml"} {
		sourcePath := filepath.Join(sourceRoot, "config", name)
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", sourcePath, err)
		}
		if err := os.WriteFile(filepath.Join(root, "config", name), content, 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	projectsYAML := []byte(`version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ` + filepath.Join(root, "odin-core") + `
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
	if err := os.WriteFile(filepath.Join(root, "config", "projects.yaml"), projectsYAML, 0o644); err != nil {
		t.Fatalf("WriteFile(projects.yaml) error = %v", err)
	}

	registryLink := filepath.Join(root, "registry")
	_ = os.RemoveAll(registryLink)
	if err := os.Symlink(filepath.Join(sourceRoot, "registry"), registryLink); err != nil {
		t.Fatalf("Symlink(registry) error = %v", err)
	}

	return root
}

func assertCountAtLeast(t *testing.T, row rowScanner, minimum int) {
	t.Helper()

	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if count < minimum {
		t.Fatalf("count = %d, want at least %d", count, minimum)
	}
}

type rowScanner interface {
	Scan(...any) error
}
