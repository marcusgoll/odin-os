package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/router"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
)

func TestCreateTaskFromActEnsuresRuntimeProjectAndCreatesQueuedTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	alpha, ok := registry.Lookup("alpha")
	if !ok {
		t.Fatalf("expected alpha project")
	}

	service := Service{
		Store:    store,
		Registry: registry,
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
		},
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Implement shell")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	if task.Status != "queued" {
		t.Fatalf("Status = %q, want queued", task.Status)
	}
	if task.Scope != string(scope.ScopeProject) {
		t.Fatalf("Scope = %q, want %q", task.Scope, scope.ScopeProject)
	}

	project, err := store.GetProjectByKey(ctx, alpha.Key)
	if err != nil {
		t.Fatalf("GetProjectByKey() error = %v", err)
	}
	if project.Key != "alpha" {
		t.Fatalf("project key = %q, want alpha", project.Key)
	}
}

func TestListFiltersJobsByScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	service := Service{
		Store:    store,
		Registry: registry,
		Now:      time.Now,
	}

	if _, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Project task"); err != nil {
		t.Fatalf("CreateTaskFromAct(alpha) error = %v", err)
	}
	if _, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeOdinCore,
		ProjectKey: "odin-core",
	}, "Core task"); err != nil {
		t.Fatalf("CreateTaskFromAct(odin-core) error = %v", err)
	}

	views, err := service.List(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(views) != 1 || views[0].ProjectKey != "alpha" {
		t.Fatalf("views = %+v, want one alpha task", views)
	}
}

func TestExecuteNextQueuedCompletesCutoverProjectTask(t *testing.T) {
	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	t.Setenv("ODIN_CODEX_DRIVER", codexDriverPath(t))
	registry := writeRegistry(t)
	git := &jobTestGit{}
	service := Service{
		Store:          store,
		Registry:       registry,
		Executors:      router.DefaultCatalog(),
		ExecutorConfig: mustLoadExecutorConfig(t),
		Transitions:    projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          git,
			WorktreeRoot: t.TempDir(),
		},
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
		},
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Execute queued task")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "completed" {
		t.Fatalf("GetTask().Status = %q, want completed", gotTask.Status)
	}

	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	if run.Status != "completed" || run.Executor != "codex_headless" {
		t.Fatalf("run = %+v, want completed codex_headless execution", run)
	}
	if run.Summary != "fixture codex driver" {
		t.Fatalf("run.Summary = %q, want fixture codex driver", run.Summary)
	}
	if git.removeWorktreeCalls != 1 {
		t.Fatalf("RemoveWorktree() calls = %d, want 1", git.removeWorktreeCalls)
	}

	lease := latestLeaseForTaskRun(t, ctx, store, task.ID, run.ID)
	if lease.State != "cleaned" {
		t.Fatalf("lease.State = %q, want cleaned", lease.State)
	}
	if lease.CleanedUpAt == nil {
		t.Fatalf("lease.CleanedUpAt = nil, want value")
	}
}

func TestExecuteNextQueuedRejectsShadowModeMutation(t *testing.T) {
	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	t.Setenv("ODIN_CODEX_DRIVER", codexDriverPath(t))
	registry := writeRegistry(t)
	service := Service{
		Store:          store,
		Registry:       registry,
		Executors:      router.DefaultCatalog(),
		ExecutorConfig: mustLoadExecutorConfig(t),
		Transitions:    projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          &jobTestGit{},
			WorktreeRoot: t.TempDir(),
		},
		Now: time.Now,
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Blocked shadow mutation")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateShadow,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(shadow) error = %v", err)
	}

	err = service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatalf("ExecuteNextQueued() error = nil, want transition denial")
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "failed" {
		t.Fatalf("GetTask().Status = %q, want failed", gotTask.Status)
	}
}

func TestExecuteNextQueuedFailsClosedOnEmptyRunStatus(t *testing.T) {
	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	t.Setenv("ODIN_CODEX_DRIVER", writeConfigurableCodexDriver(t))
	t.Setenv("ODIN_CODEX_DRIVER_RUN_RESPONSE", `{"status":"","output":"ignored"}`)
	registry := writeRegistry(t)
	git := &jobTestGit{}
	service := Service{
		Store:          store,
		Registry:       registry,
		Executors:      router.DefaultCatalog(),
		ExecutorConfig: mustLoadExecutorConfig(t),
		Transitions:    projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          git,
			WorktreeRoot: t.TempDir(),
		},
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
		},
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Malformed run status")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	err = service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatal("ExecuteNextQueued() error = nil, want invalid run status failure")
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "failed" {
		t.Fatalf("GetTask().Status = %q, want failed", gotTask.Status)
	}
	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	if run.Status != "failed" {
		t.Fatalf("Run.Status = %q, want failed", run.Status)
	}
	if git.removeWorktreeCalls != 1 {
		t.Fatalf("RemoveWorktree() calls = %d, want 1", git.removeWorktreeCalls)
	}

	lease := latestLeaseForTaskRun(t, ctx, store, task.ID, run.ID)
	if lease.State != "cleaned" {
		t.Fatalf("lease.State = %q, want cleaned", lease.State)
	}
	if lease.CleanedUpAt == nil {
		t.Fatalf("lease.CleanedUpAt = nil, want value")
	}
}

type jobTestGit struct {
	createBranchCalls   int
	addWorktreeCalls    int
	removeWorktreeCalls int
	removeRepoRoot      string
	removeWorktreePath  string
}

func (git *jobTestGit) BranchExists(context.Context, string, string) (bool, error) { return false, nil }
func (git *jobTestGit) CreateBranch(context.Context, string, string, string) error {
	git.createBranchCalls++
	return nil
}
func (git *jobTestGit) AddWorktree(context.Context, string, string, string) error {
	git.addWorktreeCalls++
	return nil
}
func (git *jobTestGit) RemoveWorktree(_ context.Context, repoRoot string, worktreePath string) error {
	git.removeWorktreeCalls++
	git.removeRepoRoot = repoRoot
	git.removeWorktreePath = worktreePath
	return nil
}

func mustLoadExecutorConfig(t *testing.T) router.Config {
	t.Helper()

	cfg, err := router.LoadConfig(filepath.Clean(filepath.Join("..", "..", "..", "config", "executors.yaml")))
	if err != nil {
		t.Fatalf("LoadConfig(executors) error = %v", err)
	}
	return cfg
}

func codexDriverPath(t *testing.T) string {
	t.Helper()

	return filepath.Clean(filepath.Join("..", "..", "..", "scripts", "drivers", "codex-headless.sh"))
}

func writeConfigurableCodexDriver(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "codex-driver.sh")
	script := `#!/usr/bin/env bash
set -euo pipefail
payload="$(cat)"
if [[ -n "${ODIN_CODEX_DRIVER_TRACE:-}" ]]; then
	printf '%s\n' "$payload" >"${ODIN_CODEX_DRIVER_TRACE}"
fi
PAYLOAD="$payload" python3 - <<'PY'
import json
import os

request = json.loads(os.environ["PAYLOAD"])
action = request.get("action")
health = os.environ.get("ODIN_CODEX_DRIVER_HEALTH_RESPONSE", '{"status":"healthy","details":"fixture codex driver healthy"}')
run = os.environ.get("ODIN_CODEX_DRIVER_RUN_RESPONSE", '{"status":"completed","output":"fixture codex driver"}')

if action == "health":
    print(health)
elif action == "run":
    print(run)
else:
    print(json.dumps({"status":"unavailable","details":f"unknown action: {action}"}))
PY
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	return path
}

func latestRunForTask(ctx context.Context, store *sqlite.Store, taskID int64) (sqlite.Run, error) {
	row := store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM runs
		WHERE task_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, taskID)

	var runID int64
	if err := row.Scan(&runID); err != nil {
		return sqlite.Run{}, err
	}
	return store.GetRun(ctx, runID)
}

func latestLeaseForTaskRun(t *testing.T, ctx context.Context, store *sqlite.Store, taskID int64, runID int64) sqlite.WorktreeLease {
	t.Helper()

	row := store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM worktree_leases
		WHERE task_id = ?
		  AND run_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, taskID, runID)

	var leaseID int64
	if err := row.Scan(&leaseID); err != nil {
		t.Fatalf("scan lease id error = %v", err)
	}

	lease, err := store.GetWorktreeLease(ctx, leaseID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	return lease
}

func openJobStore(t *testing.T) *sqlite.Store {
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
