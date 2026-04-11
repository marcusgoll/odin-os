package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/router"
	"odin-os/internal/runtime/projections"
	runsvc "odin-os/internal/runtime/runs"
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
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	service := Service{
		Store:          store,
		Registry:       registry,
		Runs:           runsvc.Service{Store: store},
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
	if !strings.Contains(run.Summary, "codex_headless_script") {
		t.Fatalf("run summary = %q, want driver-backed execution marker", run.Summary)
	}
}

func TestSchedulerContinuesAfterTaskFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	service := Service{
		Store:    store,
		Registry: registry,
		Runs:     runsvc.Service{Store: store},
		Executors: map[string]contract.Executor{
			"fake_headless": jobTestExecutor{
				runFunc: func(spec contract.TaskSpec) (contract.ExecutionResult, error) {
					if spec.Metadata["project_key"] == "beta" {
						return contract.ExecutionResult{}, fmt.Errorf("broken executor")
					}
					return contract.ExecutionResult{
						Status: "completed",
						Output: "good task complete",
					}, nil
				},
			},
		},
		ExecutorConfig: jobTestExecutorConfig(),
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

	brokenTask, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "beta",
	}, "Broken task")
	if err != nil {
		t.Fatalf("CreateTaskFromAct(beta) error = %v", err)
	}
	goodTask, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Good task")
	if err != nil {
		t.Fatalf("CreateTaskFromAct(alpha) error = %v", err)
	}

	betaProject, err := store.GetProjectByKey(ctx, "beta")
	if err != nil {
		t.Fatalf("GetProjectByKey(beta) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   betaProject.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(beta cutover) error = %v", err)
	}

	alphaProject, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   alphaProject.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(alpha cutover) error = %v", err)
	}

	err = service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatal("ExecuteNextQueued() error = nil, want isolated task failure")
	}
	if !strings.Contains(err.Error(), "broken executor") {
		t.Fatalf("ExecuteNextQueued() error = %v, want broken executor detail", err)
	}

	gotBroken, err := store.GetTask(ctx, brokenTask.ID)
	if err != nil {
		t.Fatalf("GetTask(broken) error = %v", err)
	}
	if gotBroken.Status != "failed" {
		t.Fatalf("broken task status = %q, want failed", gotBroken.Status)
	}

	gotGood, err := store.GetTask(ctx, goodTask.ID)
	if err != nil {
		t.Fatalf("GetTask(good) error = %v", err)
	}
	if gotGood.Status != "completed" {
		t.Fatalf("good task status = %q, want completed", gotGood.Status)
	}
}

func TestSchedulerRespectsPerProjectConcurrencyAndBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	service := Service{
		Store:          store,
		Registry:       registry,
		Runs:           runsvc.Service{Store: store},
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

	alphaOld, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Alpha old")
	if err != nil {
		t.Fatalf("CreateTaskFromAct(alpha old) error = %v", err)
	}
	alphaNew, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Alpha new")
	if err != nil {
		t.Fatalf("CreateTaskFromAct(alpha new) error = %v", err)
	}

	betaTask, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "beta",
	}, "Beta queued")
	if err != nil {
		t.Fatalf("CreateTaskFromAct(beta) error = %v", err)
	}

	betaProject, err := store.GetProjectByKey(ctx, "beta")
	if err != nil {
		t.Fatalf("GetProjectByKey(beta) error = %v", err)
	}
	if _, err := service.Runs.Start(ctx, betaTask, "fake_headless"); err != nil {
		t.Fatalf("Runs.Start(beta) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   betaProject.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(beta cutover) error = %v", err)
	}

	alphaProject, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   alphaProject.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(alpha cutover) error = %v", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	gotAlphaOld, err := store.GetTask(ctx, alphaOld.ID)
	if err != nil {
		t.Fatalf("GetTask(alpha old) error = %v", err)
	}
	if gotAlphaOld.Status != "completed" {
		t.Fatalf("alpha old status = %q, want completed", gotAlphaOld.Status)
	}

	gotAlphaNew, err := store.GetTask(ctx, alphaNew.ID)
	if err != nil {
		t.Fatalf("GetTask(alpha new) error = %v", err)
	}
	if gotAlphaNew.Status != "queued" {
		t.Fatalf("alpha new status = %q, want queued", gotAlphaNew.Status)
	}

	gotBeta, err := store.GetTask(ctx, betaTask.ID)
	if err != nil {
		t.Fatalf("GetTask(beta) error = %v", err)
	}
	if gotBeta.Status != "running" {
		t.Fatalf("beta status = %q, want running", gotBeta.Status)
	}
}

func TestSchedulerDemotesStalledRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	registry := writeRegistry(t)
	service := Service{
		Store:    store,
		Registry: registry,
		Runs:     runsvc.Service{Store: store},
		Executors: map[string]contract.Executor{
			"fake_headless": jobTestExecutor{
				result: contract.ExecutionResult{
					Status: "completed",
					Output: "recovered stalled run",
				},
			},
		},
		ExecutorConfig: jobTestExecutorConfig(),
		Transitions:    projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          &jobTestGit{},
			WorktreeRoot: t.TempDir(),
		},
		Now: func() time.Time { return now },
	}

	stalledTask, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Stalled alpha")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	youngerTask, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Younger alpha")
	if err != nil {
		t.Fatalf("CreateTaskFromAct(younger) error = %v", err)
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

	run, err := service.Runs.Start(ctx, stalledTask, "fake_headless")
	if err != nil {
		t.Fatalf("Runs.Start() error = %v", err)
	}

	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       stalledTask.ID,
		RunID:        run.ID,
		Mode:         "read_write",
		BranchName:   "task/stalled-alpha",
		WorktreePath: filepath.Join(t.TempDir(), "worktree"),
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	staleAt := now.Add(-2 * time.Hour)
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE runs
		SET started_at = ?
		WHERE id = ?
	`, staleAt.Format(time.RFC3339Nano), run.ID); err != nil {
		t.Fatalf("force stale run error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE worktree_leases
		SET heartbeat_at = ?, updated_at = ?
		WHERE id = ?
	`, staleAt.Format(time.RFC3339Nano), staleAt.Format(time.RFC3339Nano), lease.ID); err != nil {
		t.Fatalf("force stale lease error = %v", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	gotTask, err := store.GetTask(ctx, stalledTask.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "completed" {
		t.Fatalf("task status = %q, want completed", gotTask.Status)
	}

	gotYounger, err := store.GetTask(ctx, youngerTask.ID)
	if err != nil {
		t.Fatalf("GetTask(younger) error = %v", err)
	}
	if gotYounger.Status != "queued" {
		t.Fatalf("younger task status = %q, want queued", gotYounger.Status)
	}

	gotLease, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if gotLease.State != "released" {
		t.Fatalf("lease state = %q, want released", gotLease.State)
	}
	if gotLease.ReleasedAt == nil {
		t.Fatal("lease released_at = nil, want release timestamp")
	}

	gotRun, err := latestRunForTask(ctx, store, stalledTask.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	if gotRun.Status != "completed" {
		t.Fatalf("latest run status = %q, want completed", gotRun.Status)
	}
}

func TestSchedulerDeadLettersStalledRunsWhenRetryBudgetExhausted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	registry := writeRegistry(t)
	service := Service{
		Store:    store,
		Registry: registry,
		Runs:     runsvc.Service{Store: store},
		Executors: map[string]contract.Executor{
			"fake_headless": jobTestExecutor{
				result: contract.ExecutionResult{
					Status: "completed",
					Output: "dead-lettered task should not run",
				},
			},
		},
		ExecutorConfig: jobTestExecutorConfig(),
		Transitions:    projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          &jobTestGit{},
			WorktreeRoot: t.TempDir(),
		},
		Now: func() time.Time { return now },
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Retry exhausted")
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

	firstRun, err := service.Runs.Start(ctx, task, "fake_headless")
	if err != nil {
		t.Fatalf("Runs.Start(first) error = %v", err)
	}
	if err := service.Runs.Fail(ctx, firstRun.ID, fmt.Errorf("first failure")); err != nil {
		t.Fatalf("Runs.Fail(first) error = %v", err)
	}
	secondRun, err := service.Runs.Start(ctx, task, "fake_headless")
	if err != nil {
		t.Fatalf("Runs.Start(second) error = %v", err)
	}
	if err := service.Runs.Fail(ctx, secondRun.ID, fmt.Errorf("second failure")); err != nil {
		t.Fatalf("Runs.Fail(second) error = %v", err)
	}
	stalledRun, err := service.Runs.Start(ctx, task, "fake_headless")
	if err != nil {
		t.Fatalf("Runs.Start(stalled) error = %v", err)
	}

	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        stalledRun.ID,
		Mode:         "read_write",
		BranchName:   "task/retry-exhausted",
		WorktreePath: filepath.Join(t.TempDir(), "worktree"),
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	staleAt := now.Add(-2 * time.Hour)
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE runs
		SET started_at = ?
		WHERE id = ?
	`, staleAt.Format(time.RFC3339Nano), stalledRun.ID); err != nil {
		t.Fatalf("force stale run error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE worktree_leases
		SET heartbeat_at = ?, updated_at = ?
		WHERE id = ?
	`, staleAt.Format(time.RFC3339Nano), staleAt.Format(time.RFC3339Nano), lease.ID); err != nil {
		t.Fatalf("force stale lease error = %v", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "dead_letter" {
		t.Fatalf("task status = %q, want dead_letter", gotTask.Status)
	}
	if gotTask.TerminalReason != "stalled run retry budget exhausted" {
		t.Fatalf("task terminal reason = %q, want stalled run retry budget exhausted", gotTask.TerminalReason)
	}

	gotRun, err := store.GetRun(ctx, stalledRun.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("run status = %q, want interrupted", gotRun.Status)
	}

	gotLease, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if gotLease.State != "released" {
		t.Fatalf("lease state = %q, want released", gotLease.State)
	}
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

func TestRunNextPersistsTerminalStateAcrossTaskAndRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	service := Service{
		Store:    store,
		Registry: registry,
		Executors: map[string]contract.Executor{
			"fake_headless": jobTestExecutor{
				result: contract.ExecutionResult{
					Status: "completed",
					Output: "fake executor summary",
					Metadata: map[string]string{
						"artifacts_json": `["runs/artifacts/fake-executor.json"]`,
					},
				},
			},
		},
		ExecutorConfig: jobTestExecutorConfig(),
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

	if err := service.RunNext(ctx); err != nil {
		t.Fatalf("RunNext() error = %v", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Summary != "fake executor summary" {
		t.Fatalf("task summary = %q, want fake executor summary", gotTask.Summary)
	}
	if gotTask.TerminalReason != "completed" {
		t.Fatalf("task terminal reason = %q, want completed", gotTask.TerminalReason)
	}
	if gotTask.ArtifactsJSON != `["runs/artifacts/fake-executor.json"]` {
		t.Fatalf("task artifacts = %q, want persisted artifact pointer", gotTask.ArtifactsJSON)
	}

	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	if run.Summary != "fake executor summary" {
		t.Fatalf("run summary = %q, want fake executor summary", run.Summary)
	}
	if run.TerminalReason != "completed" {
		t.Fatalf("run terminal reason = %q, want completed", run.TerminalReason)
	}
	if run.ArtifactsJSON != `["runs/artifacts/fake-executor.json"]` {
		t.Fatalf("run artifacts = %q, want persisted artifact pointer", run.ArtifactsJSON)
	}
}

func TestRunNextRequestsApprovalForSystemProjectMutation(t *testing.T) {
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

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeOdinCore,
		ProjectKey: "odin-core",
	}, "Mutate odin core")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "odin-core")
	if err != nil {
		t.Fatalf("GetProjectByKey(odin-core) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	if err := service.RunNext(ctx); err != nil {
		t.Fatalf("RunNext() error = %v", err)
	}

	approvals, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListPendingApprovalViews() error = %v", err)
	}
	if len(approvals) != 1 {
		t.Fatalf("pending approvals = %d, want 1", len(approvals))
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "awaiting_approval" {
		t.Fatalf("task status = %q, want awaiting_approval", gotTask.Status)
	}
}

func TestRunNextFailsStartedRunWhenApprovalTransactionFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	if _, err := store.DB().ExecContext(ctx, `
		CREATE TRIGGER fail_approval_insert
		BEFORE INSERT ON approvals
		BEGIN
			SELECT RAISE(FAIL, 'approval insert blocked');
		END;
	`); err != nil {
		t.Fatalf("create approval failure trigger error = %v", err)
	}

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
		Kind:       scope.ScopeOdinCore,
		ProjectKey: "odin-core",
	}, "Mutate odin core")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "odin-core")
	if err != nil {
		t.Fatalf("GetProjectByKey(odin-core) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	err = service.RunNext(ctx)
	if err == nil {
		t.Fatal("RunNext() error = nil, want approval transaction failure")
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "failed" {
		t.Fatalf("task status = %q, want failed", gotTask.Status)
	}
	if gotTask.CurrentRunID != nil {
		t.Fatalf("task current run = %v, want cleared after failure", gotTask.CurrentRunID)
	}

	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
}

func TestRunNextPersistsLeasedWorktreeForMutableTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	service := Service{
		Store:    store,
		Registry: registry,
		Executors: map[string]contract.Executor{
			"fake_headless": jobTestExecutor{
				result: contract.ExecutionResult{
					Status: "completed",
					Output: "leased worktree complete",
				},
			},
		},
		ExecutorConfig: jobTestExecutorConfig(),
		Transitions:    projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          &jobTestGit{},
			WorktreeRoot: "~/odin-os-worktrees",
		},
		Now: time.Now,
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Mutate alpha")
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

	if err := service.RunNext(ctx); err != nil {
		t.Fatalf("RunNext() error = %v", err)
	}

	row := store.DB().QueryRowContext(ctx, `
		SELECT worktree_path
		FROM worktree_leases
		WHERE task_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, task.ID)
	var worktreePath string
	if err := row.Scan(&worktreePath); err != nil {
		t.Fatalf("Scan(worktree_path) error = %v", err)
	}
	if worktreePath == "" {
		t.Fatal("worktree path empty, want leased mutable worktree")
	}
	if worktreePath == project.GitRoot {
		t.Fatalf("worktree path = %q, want isolated path outside repo root", worktreePath)
	}
	if strings.HasPrefix(worktreePath, "~") {
		t.Fatalf("worktree path = %q, want expanded home path", worktreePath)
	}
}

func TestRunNextPassesRunIdentityToExecutor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	var captured contract.TaskSpec
	service := Service{
		Store:    store,
		Registry: registry,
		Executors: map[string]contract.Executor{
			"fake_headless": jobTestExecutor{
				result: contract.ExecutionResult{
					Status: "completed",
					Output: "captured run identity",
				},
				onRun: func(spec contract.TaskSpec) {
					captured = spec
				},
			},
		},
		ExecutorConfig: jobTestExecutorConfig(),
		Transitions:    projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          &jobTestGit{},
			WorktreeRoot: t.TempDir(),
		},
		Now: time.Now,
	}

	if _, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Capture run identity"); err != nil {
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

	if err := service.RunNext(ctx); err != nil {
		t.Fatalf("RunNext() error = %v", err)
	}

	if captured.Metadata["run_id"] == "" {
		t.Fatal("run_id metadata empty, want run identity passed to executor")
	}
	if captured.Metadata["run_attempt"] == "" {
		t.Fatal("run_attempt metadata empty, want attempt passed to executor")
	}
}

type jobTestGit struct{}

func (jobTestGit) BranchExists(context.Context, string, string) (bool, error) { return false, nil }
func (jobTestGit) CreateBranch(context.Context, string, string, string) error { return nil }
func (jobTestGit) AddWorktree(context.Context, string, string, string) error  { return nil }
func (jobTestGit) RemoveWorktree(context.Context, string, string) error       { return nil }

type jobTestExecutor struct {
	result  contract.ExecutionResult
	onRun   func(contract.TaskSpec)
	runFunc func(contract.TaskSpec) (contract.ExecutionResult, error)
}

func (jobTestExecutor) Key() string { return "fake_headless" }

func (jobTestExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (jobTestExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{Status: contract.HealthStatusHealthy}, nil
}

func (jobTestExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsResume:       true,
		SupportsCancel:       true,
		SupportsTools:        true,
		SupportsCostEstimate: true,
		SupportsHeadlessPlan: true,
		TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
		Scopes:               []string{"project"},
	}, nil
}

func (executor jobTestExecutor) RunTask(_ context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	if executor.onRun != nil {
		executor.onRun(spec)
	}
	if executor.runFunc != nil {
		return executor.runFunc(spec)
	}
	return executor.result, nil
}

func (jobTestExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, nil
}

func (jobTestExecutor) CancelTask(context.Context, contract.TaskHandle) error { return nil }

func (jobTestExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{Currency: "USD"}, nil
}

func mustLoadExecutorConfig(t *testing.T) router.Config {
	t.Helper()

	cfg, err := router.LoadConfig(filepath.Clean(filepath.Join("..", "..", "..", "config", "executors.yaml")))
	if err != nil {
		t.Fatalf("LoadConfig(executors) error = %v", err)
	}
	return cfg
}

func jobTestExecutorConfig() router.Config {
	return router.Config{
		Version: 1,
		Executors: []router.ExecutorConfig{
			{
				Key:      "fake_headless",
				Adapter:  "fake_headless",
				Class:    contract.ExecutorClassPlanBackedCLI,
				Enabled:  true,
				Priority: 1,
			},
		},
		Routes: []router.RouteConfig{
			{
				Name: "project-general",
				Match: router.RouteMatch{
					TaskKinds: []contract.TaskKind{contract.TaskKindGeneral},
					Scopes:    []string{"project"},
				},
				Preferred: []string{"fake_headless"},
			},
		},
	}
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

	for _, key := range []string{"odin-core", "alpha", "beta"} {
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
    scheduler:
      max_concurrent_runs: 1
      max_starts_per_cycle: 1
      stalled_run_retry_limit: 2
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
    scheduler:
      max_concurrent_runs: 1
      max_starts_per_cycle: 1
      stalled_run_retry_limit: 2
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
  - key: beta
    name: Beta
    project_class: github_backed_project
    git_root: beta
    default_branch: main
    scheduler:
      max_concurrent_runs: 1
      max_starts_per_cycle: 1
      stalled_run_retry_limit: 2
    github:
      repo: acme/beta
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
