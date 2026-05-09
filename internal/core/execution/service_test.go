package execution

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/core/companions"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/workitems"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/router"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
)

func TestExecuteNextQueuedCompletesCutoverProjectTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openExecutionStore(t)
	defer store.Close()

	registry := writeExecutionRegistry(t)
	task, project := queueManagedProjectTask(t, ctx, store, registry, "alpha", "Execute queued task")

	service := Service{
		Store:          store,
		Registry:       registry,
		Executors:      testExecutionExecutors(),
		ExecutorConfig: mustLoadExecutionConfig(t),
		Governance:     projects.Service{Store: store},
		WorkItems:      workitems.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          &executionTestGit{},
			WorktreeRoot: t.TempDir(),
		},
	}

	if _, err := service.Governance.SetTransitionState(ctx, projects.TransitionStateInput{
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
}

func TestExecuteNextQueuedRejectsShadowModeMutation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openExecutionStore(t)
	defer store.Close()

	registry := writeExecutionRegistry(t)
	task, project := queueManagedProjectTask(t, ctx, store, registry, "alpha", "Blocked shadow mutation")

	service := Service{
		Store:          store,
		Registry:       registry,
		Executors:      testExecutionExecutors(),
		ExecutorConfig: mustLoadExecutionConfig(t),
		Governance:     projects.Service{Store: store},
		WorkItems:      workitems.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          &executionTestGit{},
			WorktreeRoot: t.TempDir(),
		},
	}

	if _, err := service.Governance.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateShadow,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(shadow) error = %v", err)
	}

	err := service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatal("ExecuteNextQueued() error = nil, want transition denial")
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "failed" {
		t.Fatalf("GetTask().Status = %q, want failed", gotTask.Status)
	}
}

func TestExecuteNextQueuedPreservesBlockedTaskWhenApprovalGateTripsBeforeStart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openExecutionStore(t)
	defer store.Close()

	registry := writeExecutionRegistry(t)
	task, project := queueManagedProjectTask(t, ctx, store, registry, "alpha", "Approval race task")

	service := Service{
		Store:          store,
		Registry:       registry,
		Executors:      testExecutionExecutors(),
		ExecutorConfig: mustLoadExecutionConfig(t),
		Governance:     projects.Service{Store: store},
		WorkItems:      workitems.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          &executionTestGit{},
			WorktreeRoot: t.TempDir(),
		},
	}

	if _, err := service.Governance.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		CREATE TRIGGER runs_insert_blocks_task
		AFTER INSERT ON runs
		BEGIN
			UPDATE tasks
			SET status = 'blocked'
			WHERE id = NEW.task_id;
			INSERT INTO approvals (task_id, run_id, status, requested_at, resolved_at, decision_by, reason)
			VALUES (
				NEW.task_id,
				NEW.id,
				'pending',
				strftime('%Y-%m-%dT%H:%M:%fZ','now'),
				NULL,
				'',
				''
			);
		END;
	`); err != nil {
		t.Fatalf("create trigger error = %v", err)
	}

	err := service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatal("ExecuteNextQueued() error = nil, want blocked start rejection")
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "blocked" {
		t.Fatalf("GetTask().Status = %q, want blocked", gotTask.Status)
	}
}

type executionTestGit struct{}

func (executionTestGit) BranchExists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (executionTestGit) CreateBranch(context.Context, string, string, string) error { return nil }
func (executionTestGit) AddWorktree(context.Context, string, string, string) error  { return nil }
func (executionTestGit) RemoveWorktree(context.Context, string, string) error       { return nil }
func (executionTestGit) WorktreeDirty(context.Context, string) (bool, error)        { return false, nil }

func openExecutionStore(t *testing.T) *sqlite.Store {
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

func writeExecutionRegistry(t *testing.T) projects.Registry {
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

func queueManagedProjectTask(t *testing.T, ctx context.Context, store *sqlite.Store, registry projects.Registry, projectKey, title string) (sqlite.Task, sqlite.Project) {
	t.Helper()

	manifest, ok := registry.Lookup(projectKey)
	if !ok {
		t.Fatalf("Lookup(%s) = missing", projectKey)
	}

	governance := projects.Service{Store: store}
	project, err := governance.RegisterManagedProject(ctx, manifest)
	if err != nil {
		t.Fatalf("RegisterManagedProject(%s) error = %v", projectKey, err)
	}

	workspace, err := workspaces.Service{Store: store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	companion, err := companions.Service{Store: store}.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey() error = %v", err)
	}
	initiative, err := governance.RegisterManagedProjectInitiative(ctx, workspace.ID, project, &companion.ID)
	if err != nil {
		t.Fatalf("RegisterManagedProjectInitiative() error = %v", err)
	}

	task, err := workitems.Service{Store: store}.Queue(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "task-" + projectKey,
		Title:        title,
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "project",
	})
	if err != nil {
		t.Fatalf("Queue() error = %v", err)
	}

	return task, project
}

func mustLoadExecutionConfig(t *testing.T) router.Config {
	t.Helper()

	cfg, err := router.LoadConfig(filepath.Clean(filepath.Join("..", "..", "..", "config", "executors.yaml")))
	if err != nil {
		t.Fatalf("LoadConfig(executors) error = %v", err)
	}
	return cfg
}

func testExecutionExecutors() map[string]contract.Executor {
	return map[string]contract.Executor{
		"codex_headless": executionTestExecutor{
			key: "codex_headless",
			result: contract.ExecutionResult{
				Status: "completed",
				Output: "task complete",
			},
		},
	}
}

type executionTestExecutor struct {
	key    string
	result contract.ExecutionResult
}

func (executor executionTestExecutor) Key() string { return executor.key }

func (executor executionTestExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (executor executionTestExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{Status: contract.HealthStatusHealthy}, nil
}

func (executor executionTestExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsHeadlessPlan: true,
		TaskKinds: []contract.TaskKind{
			contract.TaskKindGeneral,
			contract.TaskKindPlan,
			contract.TaskKindBuild,
			contract.TaskKindReview,
			contract.TaskKindQA,
			contract.TaskKindResearch,
		},
		Scopes: []string{"global", "odin-core", "project", "new-project"},
	}, nil
}

func (executor executionTestExecutor) RunTask(context.Context, contract.TaskSpec) (contract.ExecutionResult, error) {
	return executor.result, nil
}

func (executionTestExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (executionTestExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (executionTestExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}
