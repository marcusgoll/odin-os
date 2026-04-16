package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/router"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
	"odin-os/internal/vcs/worktrees"
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

func TestExecuteNextQueuedTargetsCleanupToFinishedAssignment(t *testing.T) {
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
	}, "Targeted cleanup")
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

	unrelatedTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-unrelated",
		Title:       "Released worktree",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(unrelated) error = %v", err)
	}
	unrelatedRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   unrelatedTask.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(unrelated) error = %v", err)
	}
	unrelatedLease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       unrelatedTask.ID,
		RunID:        unrelatedRun.ID,
		Mode:         "mutable",
		BranchName:   "odin/alpha/task-999/run-999/try-1",
		WorktreePath: filepath.Join(t.TempDir(), "released-worktree"),
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(unrelated) error = %v", err)
	}
	if _, err := store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: unrelatedLease.ID,
		State:   "released",
	}); err != nil {
		t.Fatalf("ReleaseWorktreeLease(unrelated) error = %v", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}
	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}

	if git.removeWorktreeCalls != 1 {
		t.Fatalf("RemoveWorktree() calls = %d, want 1", git.removeWorktreeCalls)
	}
	if len(git.removeWorktreePaths) != 1 || git.removeWorktreePaths[0] != git.removeWorktreePath {
		t.Fatalf("RemoveWorktree paths = %+v, want only finished assignment path %q", git.removeWorktreePaths, git.removeWorktreePath)
	}

	lease := latestLeaseForTaskRun(t, ctx, store, task.ID, run.ID)
	if lease.State != "cleaned" {
		t.Fatalf("finished lease state = %q, want cleaned", lease.State)
	}
	unrelatedAfter := latestLeaseByID(t, ctx, store, unrelatedLease.ID)
	if unrelatedAfter.State != "released" {
		t.Fatalf("unrelated lease state = %q, want released", unrelatedAfter.State)
	}
	if unrelatedAfter.CleanedUpAt != nil {
		t.Fatalf("unrelated lease cleaned up unexpectedly")
	}
}

func TestExecuteNextQueuedHeartbeatsMutableLeaseWhileRunning(t *testing.T) {
	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	clock := &testClock{now: time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)}
	store.Now = clock.Now

	registry := writeRegistry(t)
	git := &jobTestGit{}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	service := Service{
		Store:    store,
		Registry: registry,
		Executors: map[string]contract.Executor{
			"blocking": &blockingExecutor{
				started: started,
				release: release,
			},
		},
		ExecutorConfig: router.Config{
			Version: 1,
			Executors: []router.ExecutorConfig{
				{
					Key:      "blocking",
					Adapter:  "test",
					Class:    contract.ExecutorClassPlanBackedCLI,
					Enabled:  true,
					Priority: 1,
				},
			},
			Routes: []router.RouteConfig{
				{
					Name: "default",
					Match: router.RouteMatch{
						TaskKinds: []contract.TaskKind{contract.TaskKindGeneral},
						Scopes:    []string{"project"},
					},
					Preferred: []string{"blocking"},
				},
			},
		},
		Transitions: projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          git,
			WorktreeRoot: t.TempDir(),
		},
		MutableHeartbeatInterval: 10 * time.Millisecond,
		Now:                      clock.Now,
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Heartbeat mutable lease")
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

	runDone := make(chan error, 1)
	go func() {
		runDone <- service.ExecuteNextQueued(ctx)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not start")
	}

	clock.Set(clock.NowValue().Add(31 * time.Minute))

	updatedLease, err := waitForLeaseHeartbeatAfter(t, ctx, store, task.ID, clock.NowValue().Add(-30*time.Minute), 2*time.Second)
	if err != nil {
		close(release)
		<-runDone
		t.Fatal(err)
	}
	if updatedLease.State != "active" {
		t.Fatalf("lease.State = %q, want active while run is in progress", updatedLease.State)
	}

	manager := worktrees.Manager{
		Store: store,
		Git:   git,
	}
	result, err := manager.Cleanup(ctx, clock.NowValue().Add(-30*time.Minute))
	if err != nil {
		close(release)
		<-runDone
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(result.Removed) != 0 {
		close(release)
		<-runDone
		t.Fatalf("Cleanup().Removed len = %d, want 0 for active lease", len(result.Removed))
	}
	if git.removeWorktreeCalls != 0 {
		close(release)
		<-runDone
		t.Fatalf("RemoveWorktree() calls = %d, want 0 while run is active", git.removeWorktreeCalls)
	}

	close(release)
	if err := <-runDone; err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	if git.removeWorktreeCalls != 1 {
		t.Fatalf("RemoveWorktree() calls = %d, want 1 after completion", git.removeWorktreeCalls)
	}
	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	finishedLease := latestLeaseForTaskRun(t, ctx, store, task.ID, run.ID)
	if finishedLease.State != "cleaned" {
		t.Fatalf("finished lease state = %q, want cleaned", finishedLease.State)
	}
	if finishedLease.CleanedUpAt == nil {
		t.Fatalf("finished lease cleaned up at = nil, want value")
	}
}

func TestExecuteNextQueuedPreservesMutableLeaseWhenTerminalPersistenceFails(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	defer store.Close()

	registry := writeRegistry(t)
	git := &jobTestGit{}
	service := Service{
		Store:    store,
		Registry: registry,
		Executors: map[string]contract.Executor{
			"closing": &closingExecutor{store: store},
		},
		ExecutorConfig: router.Config{
			Version: 1,
			Executors: []router.ExecutorConfig{
				{
					Key:      "closing",
					Adapter:  "test",
					Class:    contract.ExecutorClassPlanBackedCLI,
					Enabled:  true,
					Priority: 1,
				},
			},
			Routes: []router.RouteConfig{
				{
					Name: "default",
					Match: router.RouteMatch{
						TaskKinds: []contract.TaskKind{contract.TaskKindGeneral},
						Scopes:    []string{"project"},
					},
					Preferred: []string{"closing"},
				},
			},
		},
		Transitions: projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          git,
			WorktreeRoot: t.TempDir(),
		},
		Now: time.Now,
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Persist terminal state")
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
		t.Fatal("ExecuteNextQueued() error = nil, want terminal persistence failure")
	}
	if git.removeWorktreeCalls != 0 {
		t.Fatalf("RemoveWorktree() calls = %d, want 0 when terminal persistence fails", git.removeWorktreeCalls)
	}

	reopened, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store error = %v", err)
	}
	defer reopened.Close()

	run, err := latestRunForTask(ctx, reopened, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	if run.Status != "running" {
		t.Fatalf("run.Status = %q, want running when terminal persistence fails", run.Status)
	}

	lease := latestLeaseForTaskRun(t, ctx, reopened, task.ID, run.ID)
	if lease.State != "active" {
		t.Fatalf("lease.State = %q, want active when terminal persistence fails", lease.State)
	}
	if lease.CleanedUpAt != nil {
		t.Fatalf("lease.CleanedUpAt = %v, want nil when terminal persistence fails", lease.CleanedUpAt)
	}
}

func TestExecuteNextQueuedRollsBackTerminalPersistenceWhenTaskUpdateFails(t *testing.T) {
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
		Now: time.Now,
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Rollback terminal persistence")
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

	triggerName := fmt.Sprintf("fail_task_status_%d", task.ID)
	if _, err := store.DB().ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER %s
		BEFORE UPDATE OF status ON tasks
		FOR EACH ROW
		WHEN NEW.id = %d AND NEW.status = 'completed'
		BEGIN
			SELECT RAISE(FAIL, 'blocked task completion');
		END;
	`, triggerName, task.ID)); err != nil {
		t.Fatalf("create trigger error = %v", err)
	}

	err = service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatal("ExecuteNextQueued() error = nil, want blocked task completion")
	}
	if git.removeWorktreeCalls != 0 {
		t.Fatalf("RemoveWorktree() calls = %d, want 0 when terminal persistence rolls back", git.removeWorktreeCalls)
	}

	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	if run.Status != "running" {
		t.Fatalf("run.Status = %q, want running after rolled-back terminal persistence", run.Status)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "running" {
		t.Fatalf("task.Status = %q, want running after rolled-back terminal persistence", gotTask.Status)
	}
	if gotTask.CurrentRunID == nil || *gotTask.CurrentRunID != run.ID {
		t.Fatalf("task.CurrentRunID = %v, want %d", gotTask.CurrentRunID, run.ID)
	}

	lease := latestLeaseForTaskRun(t, ctx, store, task.ID, run.ID)
	if lease.State != "active" {
		t.Fatalf("lease.State = %q, want active after rolled-back terminal persistence", lease.State)
	}
	if lease.CleanedUpAt != nil {
		t.Fatalf("lease.CleanedUpAt = %v, want nil after rolled-back terminal persistence", lease.CleanedUpAt)
	}
}

func TestExecuteNextQueuedRollsBackRunLaunchWhenTaskStatusUpdateFails(t *testing.T) {
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
		Now: time.Now,
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Rollback run launch")
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

	triggerName := fmt.Sprintf("fail_task_launch_%d", task.ID)
	if _, err := store.DB().ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER %s
		BEFORE UPDATE OF status ON tasks
		FOR EACH ROW
		WHEN NEW.id = %d AND NEW.status = 'running'
		BEGIN
			SELECT RAISE(FAIL, 'blocked task launch');
		END;
	`, triggerName, task.ID)); err != nil {
		t.Fatalf("create trigger error = %v", err)
	}

	err = service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatal("ExecuteNextQueued() error = nil, want blocked task launch")
	}
	if git.removeWorktreeCalls != 0 {
		t.Fatalf("RemoveWorktree() calls = %d, want 0 when run launch rolls back", git.removeWorktreeCalls)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("task.Status = %q, want queued after rolled-back run launch", gotTask.Status)
	}
	if gotTask.CurrentRunID != nil {
		t.Fatalf("task.CurrentRunID = %v, want nil after rolled-back run launch", gotTask.CurrentRunID)
	}

	row := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM runs
		WHERE task_id = ?
	`, task.ID)
	var runCount int
	if err := row.Scan(&runCount); err != nil {
		t.Fatalf("scan run count error = %v", err)
	}
	if runCount != 0 {
		t.Fatalf("run count = %d, want 0 after rolled-back run launch", runCount)
	}
}

type jobTestGit struct {
	createBranchCalls   int
	addWorktreeCalls    int
	removeWorktreeCalls int
	removeRepoRoot      string
	removeWorktreePath  string
	removeWorktreePaths []string
	removeWorktreeErrs  map[string]error
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
	git.removeWorktreePaths = append(git.removeWorktreePaths, worktreePath)
	if git.removeWorktreeErrs != nil {
		if err, ok := git.removeWorktreeErrs[worktreePath]; ok {
			return err
		}
	}
	return nil
}

type blockingExecutor struct {
	started chan struct{}
	release chan struct{}
}

type closingExecutor struct {
	store *sqlite.Store
}

func (executor *blockingExecutor) Key() string { return "blocking" }
func (executor *blockingExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}
func (executor *blockingExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()}, nil
}
func (executor *blockingExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsHeadlessPlan: true,
		TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
		Scopes:               []string{"project"},
	}, nil
}
func (executor *blockingExecutor) RunTask(ctx context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	select {
	case executor.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return contract.ExecutionResult{Status: "interrupted"}, ctx.Err()
	case <-executor.release:
		return contract.ExecutionResult{Status: "completed", Output: "done"}, nil
	}
}
func (executor *closingExecutor) Key() string { return "closing" }
func (executor *closingExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}
func (executor *closingExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()}, nil
}
func (executor *closingExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsHeadlessPlan: true,
		TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
		Scopes:               []string{"project"},
	}, nil
}
func (executor *closingExecutor) RunTask(context.Context, contract.TaskSpec) (contract.ExecutionResult, error) {
	if executor.store != nil {
		_ = executor.store.Close()
	}
	return contract.ExecutionResult{Status: "completed", Output: "done"}, nil
}
func (executor *closingExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}
func (executor *closingExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}
func (executor *closingExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}
func (executor *blockingExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}
func (executor *blockingExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}
func (executor *blockingExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) Set(now time.Time) {
	clock.mu.Lock()
	clock.now = now
	clock.mu.Unlock()
}

func (clock *testClock) NowValue() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
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

func latestLeaseByID(t *testing.T, ctx context.Context, store *sqlite.Store, leaseID int64) sqlite.WorktreeLease {
	t.Helper()

	lease, err := store.GetWorktreeLease(ctx, leaseID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	return lease
}

func waitForLeaseHeartbeatAfter(t *testing.T, ctx context.Context, store *sqlite.Store, taskID int64, staleBefore time.Time, timeout time.Duration) (sqlite.WorktreeLease, error) {
	t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline.C:
			return sqlite.WorktreeLease{}, fmt.Errorf("timed out waiting for heartbeat after %s", timeout)
		case <-ticker.C:
			row := store.DB().QueryRowContext(ctx, `
				SELECT id
				FROM worktree_leases
				WHERE task_id = ?
				ORDER BY id DESC
				LIMIT 1
			`, taskID)
			var leaseID int64
			if err := row.Scan(&leaseID); err != nil {
				continue
			}
			lease, err := store.GetWorktreeLease(ctx, leaseID)
			if err != nil {
				continue
			}
			if lease.HeartbeatAt.After(staleBefore) {
				return lease, nil
			}
		}
	}
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
