package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/projects"
	corescope "odin-os/internal/core/scope"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/router"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
)

func TestFinalizeTaskOutcomeDelegatesRawExecutorStatusToWorkItems(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	finalizer := &recordingTaskFinalizer{}

	if err := finalizeTaskOutcome(ctx, finalizer, 42, contract.ExecutionResult{Status: "timed_out"}); err != nil {
		t.Fatalf("finalizeTaskOutcome() error = %v", err)
	}

	if finalizer.taskID != 42 {
		t.Fatalf("finalizer.taskID = %d, want 42", finalizer.taskID)
	}
	if finalizer.status != "timed_out" {
		t.Fatalf("finalizer.status = %q, want timed_out", finalizer.status)
	}
}

func TestExecuteNextQueuedDelegatesToExecutionService(t *testing.T) {
	t.Parallel()

	executor := &recordingQueueExecutor{err: context.Canceled}
	service := Service{Execution: executor}

	err := service.ExecuteNextQueued(context.Background())
	if err != context.Canceled {
		t.Fatalf("ExecuteNextQueued() error = %v, want %v", err, context.Canceled)
	}
	if !executor.called {
		t.Fatal("ExecuteNextQueued() did not delegate to execution service")
	}
}

func TestResolutionCreateTaskFromActUsesControlScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	service := Service{
		Store:    store,
		Registry: registry,
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
		},
	}

	task, err := service.CreateTaskFromAct(ctx, corescope.ControlScope{
		SubjectType:   corescope.SubjectTypeInitiative,
		SubjectKey:    "alpha",
		WorkspaceKey:  workspaces.DefaultWorkspaceKey,
		InitiativeKey: "alpha",
		ProjectKey:    "alpha",
		CompanionKey:  workspaces.DefaultWorkspaceCompanionKey,
	}, "Implement shell")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	if task.Scope != "project" {
		t.Fatalf("Scope = %q, want project", task.Scope)
	}
}

func TestResolutionListFiltersJobsByControlScope(t *testing.T) {
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

	if _, err := service.CreateTaskFromAct(ctx, corescope.ControlScope{
		SubjectType:   corescope.SubjectTypeInitiative,
		SubjectKey:    "alpha",
		WorkspaceKey:  workspaces.DefaultWorkspaceKey,
		InitiativeKey: "alpha",
		ProjectKey:    "alpha",
		CompanionKey:  workspaces.DefaultWorkspaceCompanionKey,
	}, "Project task"); err != nil {
		t.Fatalf("CreateTaskFromAct(alpha) error = %v", err)
	}
	if _, err := service.CreateTaskFromAct(ctx, corescope.ControlScope{
		SubjectType:   corescope.SubjectTypeNewProject,
		SubjectKey:    "odin-core",
		WorkspaceKey:  workspaces.DefaultWorkspaceKey,
		InitiativeKey: "odin-core",
		ProjectKey:    "odin-core",
		CompanionKey:  workspaces.DefaultWorkspaceCompanionKey,
	}, "New project task"); err != nil {
		t.Fatalf("CreateTaskFromAct(new-project) error = %v", err)
	}

	views, err := service.List(ctx, corescope.ControlScope{
		SubjectType:   corescope.SubjectTypeInitiative,
		SubjectKey:    "alpha",
		WorkspaceKey:  workspaces.DefaultWorkspaceKey,
		InitiativeKey: "alpha",
		ProjectKey:    "alpha",
		CompanionKey:  workspaces.DefaultWorkspaceCompanionKey,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(views) != 1 || views[0].ProjectKey != "alpha" {
		t.Fatalf("views = %+v, want one alpha task", views)
	}
}

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

	task, err := service.CreateTaskFromAct(ctx, scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey: "alpha",
		},
	}).ControlScope(), "Implement shell")
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

	workspace, err := workspaces.Service{Store: store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}

	initiative, err := store.GetInitiativeByKey(ctx, workspace.ID, alpha.Key)
	if err != nil {
		t.Fatalf("GetInitiativeByKey(alpha) error = %v", err)
	}
	if initiative.Kind != string(initiatives.KindManagedProject) {
		t.Fatalf("initiative.Kind = %q, want %q", initiative.Kind, initiatives.KindManagedProject)
	}
	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("initiative.LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}
}

func TestCreateTaskFromActRepairsExistingDefaultWorkspace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	workspace, err := workspaces.Service{Store: store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		DELETE FROM companions
		WHERE workspace_id = ? AND key = ?
	`, workspace.ID, workspace.DefaultCompanionKey); err != nil {
		t.Fatalf("delete default companion error = %v", err)
	}

	registry := writeRegistry(t)
	alpha, ok := registry.Lookup("alpha")
	if !ok {
		t.Fatalf("expected alpha project")
	}

	service := Service{
		Store:    store,
		Registry: registry,
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 12, 30, 0, 0, time.UTC)
		},
	}

	resolved := scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey: "alpha",
		},
	}).ControlScope()

	workspaceFromAct, err := service.actWorkspace(ctx, resolved)
	if err != nil {
		t.Fatalf("actWorkspace() error = %v", err)
	}

	companion, err := service.actCompanion(ctx, workspaceFromAct, resolved)
	if err != nil {
		t.Fatalf("actCompanion() error = %v", err)
	}
	if companion.Key != workspace.DefaultCompanionKey {
		t.Fatalf("companion.Key = %q, want %q", companion.Key, workspace.DefaultCompanionKey)
	}

	task, err := service.CreateTaskFromAct(ctx, resolved, "Repair existing workspace")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}
	if task.Status != "queued" {
		t.Fatalf("CreateTaskFromAct().Status = %q, want queued", task.Status)
	}

	initiative, err := store.GetInitiativeByKey(ctx, workspace.ID, alpha.Key)
	if err != nil {
		t.Fatalf("GetInitiativeByKey(alpha) error = %v", err)
	}
	if initiative.Kind != string(initiatives.KindManagedProject) {
		t.Fatalf("initiative.Kind = %q, want %q", initiative.Kind, initiatives.KindManagedProject)
	}
}

func TestCreateTaskFromActPopulatesSemanticLinks(t *testing.T) {
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

	task, err := service.CreateTaskFromAct(ctx, scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey: "alpha",
		},
	}).ControlScope(), "Implement shell")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	workspace, err := workspaces.Service{Store: store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey() error = %v", err)
	}
	initiative, err := store.GetInitiativeByKey(ctx, workspace.ID, alpha.Key)
	if err != nil {
		t.Fatalf("GetInitiativeByKey() error = %v", err)
	}

	if task.WorkspaceID == nil || *task.WorkspaceID != workspace.ID {
		t.Fatalf("WorkspaceID = %v, want %d", task.WorkspaceID, workspace.ID)
	}
	if task.InitiativeID == nil || *task.InitiativeID != initiative.ID {
		t.Fatalf("InitiativeID = %v, want %d", task.InitiativeID, initiative.ID)
	}
	if task.CompanionID == nil || *task.CompanionID != companion.ID {
		t.Fatalf("CompanionID = %v, want %d", task.CompanionID, companion.ID)
	}
	if task.WorkKind != string(scope.ScopeProject) {
		t.Fatalf("WorkKind = %q, want %q", task.WorkKind, scope.ScopeProject)
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

	if _, err := service.CreateTaskFromAct(ctx, scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey: "alpha",
		},
	}).ControlScope(), "Project task"); err != nil {
		t.Fatalf("CreateTaskFromAct(alpha) error = %v", err)
	}
	if _, err := service.CreateTaskFromAct(ctx, scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey:    "odin-core",
			SystemProject: true,
		},
	}).ControlScope(), "Core task"); err != nil {
		t.Fatalf("CreateTaskFromAct(odin-core) error = %v", err)
	}

	views, err := service.List(ctx, scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey: "alpha",
		},
	}).ControlScope())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(views) != 1 || views[0].ProjectKey != "alpha" {
		t.Fatalf("views = %+v, want one alpha task", views)
	}
}

func TestExecuteNextQueuedCompletesCutoverProjectTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

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
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
		},
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey: "alpha",
		},
	}).ControlScope(), "Execute queued task")
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
}

func TestExecuteNextQueuedRollsBackRunWhenWorkItemStartFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

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
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
		},
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey: "alpha",
		},
	}).ControlScope(), "Start failure task")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		CREATE TRIGGER task_start_blocker
		BEFORE UPDATE OF status ON tasks
		WHEN NEW.status = 'running'
		BEGIN
			SELECT RAISE(ABORT, 'start blocked');
		END;
	`); err != nil {
		t.Fatalf("create trigger error = %v", err)
	}

	err = service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatal("ExecuteNextQueued() error = nil, want start failure")
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "failed" {
		t.Fatalf("GetTask().Status = %q, want failed", gotTask.Status)
	}
	if gotTask.CurrentRunID != nil {
		t.Fatalf("GetTask().CurrentRunID = %v, want nil", gotTask.CurrentRunID)
	}

	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	if run.Status != "failed" {
		t.Fatalf("run.Status = %q, want failed", run.Status)
	}
}

func TestExecuteNextQueuedPreservesBlockedTaskWhenApprovalGateTripsBeforeStart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

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
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
		},
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey: "alpha",
		},
	}).ControlScope(), "Approval race task")
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

	err = service.ExecuteNextQueued(ctx)
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

	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	if run.Status != "interrupted" {
		t.Fatalf("run.Status = %q, want interrupted", run.Status)
	}

	var approvalCount int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM approvals
		WHERE task_id = ? AND status = 'pending'
	`, task.ID).Scan(&approvalCount); err != nil {
		t.Fatalf("count pending approvals error = %v", err)
	}
	if approvalCount != 1 {
		t.Fatalf("pending approval count = %d, want 1", approvalCount)
	}
}

func TestExecuteNextQueuedAbandonsRunWhenTaskCompletesBeforeStart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

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
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
		},
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey: "alpha",
		},
	}).ControlScope(), "Completed before start")
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

	if _, err := store.DB().ExecContext(ctx, `
		CREATE TRIGGER runs_insert_completes_task
		AFTER INSERT ON runs
		BEGIN
			UPDATE tasks
			SET status = 'completed'
			WHERE id = NEW.task_id;
		END;
	`); err != nil {
		t.Fatalf("create trigger error = %v", err)
	}

	err = service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatal("ExecuteNextQueued() error = nil, want stale-read rejection")
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
	if run.Status != "interrupted" {
		t.Fatalf("run.Status = %q, want interrupted", run.Status)
	}
}

type recordingTaskFinalizer struct {
	taskID int64
	status string
}

type recordingQueueExecutor struct {
	called bool
	err    error
}

func (executor *recordingQueueExecutor) ExecuteNextQueued(context.Context) error {
	executor.called = true
	return executor.err
}

func (finalizer *recordingTaskFinalizer) Finalize(_ context.Context, taskID int64, status string) (sqlite.Task, error) {
	finalizer.taskID = taskID
	finalizer.status = status
	return sqlite.Task{ID: taskID, Status: status}, nil
}

func TestExecuteNextQueuedRejectsShadowModeMutation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

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

	task, err := service.CreateTaskFromAct(ctx, scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey: "alpha",
		},
	}).ControlScope(), "Blocked shadow mutation")
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

type jobTestGit struct{}

func (jobTestGit) BranchExists(context.Context, string, string) (bool, error) { return false, nil }
func (jobTestGit) CreateBranch(context.Context, string, string, string) error { return nil }
func (jobTestGit) AddWorktree(context.Context, string, string, string) error  { return nil }
func (jobTestGit) RemoveWorktree(context.Context, string, string) error       { return nil }

func mustLoadExecutorConfig(t *testing.T) router.Config {
	t.Helper()

	cfg, err := router.LoadConfig(filepath.Clean(filepath.Join("..", "..", "..", "config", "executors.yaml")))
	if err != nil {
		t.Fatalf("LoadConfig(executors) error = %v", err)
	}
	return cfg
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
