package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/router"
	"odin-os/internal/runtime/checkpoints"
	runtimeevents "odin-os/internal/runtime/events"
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
	recordHealthyExecutorSample(t, ctx, store)

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

func TestExecuteNextQueuedSkipsDispatchWhenShutdownRequested(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	var shutdownRequested atomic.Bool
	shutdownRequested.Store(true)
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
		ShutdownRequested: &shutdownRequested,
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
		},
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Dispatch should not start during shutdown")
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
	recordHealthyExecutorSample(t, ctx, store)

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("GetTask().Status = %q, want queued", gotTask.Status)
	}
	if _, err := latestRunForTask(ctx, store, task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("latestRunForTask() error = %v, want sql.ErrNoRows", err)
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
	recordHealthyExecutorSample(t, ctx, store)

	err = service.ExecuteNextQueued(ctx)
	if err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v, want nil admission failure", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "failed" {
		t.Fatalf("GetTask().Status = %q, want failed", gotTask.Status)
	}

	if _, err := latestRunForTask(ctx, store, task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("latestRunForTask() error = %v, want sql.ErrNoRows", err)
	}
}

func TestExecuteNextQueuedBlocksWhenExecutorHealthIsDegraded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
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
		Now: func() time.Time { return now },
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Blocked by executor health")
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
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "degraded",
		LatencyMS:   0,
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v, want nil blocked outcome", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "blocked" {
		t.Fatalf("Task.Status = %q, want blocked", gotTask.Status)
	}
	if gotTask.BlockedReason != "executor_unavailable" {
		t.Fatalf("BlockedReason = %q, want executor_unavailable", gotTask.BlockedReason)
	}
	packet, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerIdlePause) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerIdlePause)
	}
	resumeState, err := checkpoints.Service{Store: store}.LoadResumeState(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.BlockingReason != "executor_unavailable" {
		t.Fatalf("ResumeState.BlockingReason = %q, want %q", resumeState.BlockingReason, "executor_unavailable")
	}
	if len(resumeState.NextSteps) == 0 {
		t.Fatalf("ResumeState.NextSteps = %v, want at least one step", resumeState.NextSteps)
	}
	if _, err := latestRunForTask(ctx, store, task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("latestRunForTask() error = %v, want sql.ErrNoRows", err)
	}
}

func TestInterruptDispatchCreatesResumableWakePacket(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	service := Service{
		Store:    store,
		Registry: registry,
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha-runtime",
		Name:          "Alpha Runtime",
		Scope:         "project",
		GitRoot:       "/tmp/alpha-runtime",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "interrupted-dispatch",
		Title:       "Interrupted dispatch",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     task.ID,
		Executor:   "codex_headless",
		Attempt:    1,
		Status:     "running",
		TaskStatus: "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	if err := service.interruptDispatch(ctx, run.ID); err != nil {
		t.Fatalf("interruptDispatch() error = %v", err)
	}

	packet, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerHandoff) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerHandoff)
	}
	resumeState, err := checkpoints.Service{Store: store}.LoadResumeState(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.Status != "queued" {
		t.Fatalf("ResumeState.Status = %q, want queued", resumeState.Status)
	}
	if resumeState.RunContext == nil || resumeState.RunContext.RunID != run.ID {
		t.Fatalf("ResumeState.RunContext = %+v, want run %d", resumeState.RunContext, run.ID)
	}
	if len(resumeState.NextSteps) == 0 {
		t.Fatalf("ResumeState.NextSteps = %v, want at least one step", resumeState.NextSteps)
	}
}

func TestExecuteNextQueuedBlocksWhenExecutorHealthSampleIsMissing(t *testing.T) {
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
	}, "Blocked by missing executor health")
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
		t.Fatalf("ExecuteNextQueued() error = %v, want nil blocked outcome", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "blocked" {
		t.Fatalf("Task.Status = %q, want blocked", gotTask.Status)
	}
	if gotTask.BlockedReason != "executor_unavailable" {
		t.Fatalf("BlockedReason = %q, want executor_unavailable", gotTask.BlockedReason)
	}
	if _, err := latestRunForTask(ctx, store, task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("latestRunForTask() error = %v, want sql.ErrNoRows", err)
	}
}

func TestExecuteNextQueuedFailsWhenExecutorSelectionHasNoRoute(t *testing.T) {
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

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           alpha.Key,
		Name:          alpha.Name,
		Scope:         "project",
		GitRoot:       alpha.GitRoot,
		DefaultBranch: alpha.DefaultBranch,
		GitHubRepo:    alpha.GitHub.Repo,
		ManifestPath:  alpha.SourcePath,
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "no-route-task",
		Title:       "No route available",
		Status:      "queued",
		Scope:       "unsupported-scope",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "failed" {
		t.Fatalf("Task.Status = %q, want failed", gotTask.Status)
	}
	if gotTask.LastError == "" {
		t.Fatalf("LastError = empty, want selector failure detail")
	}
	if _, err := latestRunForTask(ctx, store, task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("latestRunForTask() error = %v, want sql.ErrNoRows", err)
	}
}

func TestExecuteNextQueuedBlocksWhenRequiredApprovalIsMissing(t *testing.T) {
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
	}, "Requires approval")
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

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v, want nil blocked outcome", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "blocked" {
		t.Fatalf("Task.Status = %q, want blocked", gotTask.Status)
	}
	if gotTask.BlockedReason != "approval_required" {
		t.Fatalf("BlockedReason = %q, want approval_required", gotTask.BlockedReason)
	}

	approval, err := store.GetLatestTaskApproval(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskApproval() error = %v", err)
	}
	if approval.Status != "pending" {
		t.Fatalf("Approval.Status = %q, want pending", approval.Status)
	}
	packet, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerApprovalWait) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerApprovalWait)
	}
	resumeState, err := checkpoints.Service{Store: store}.LoadResumeState(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.BlockingReason != "approval_required" {
		t.Fatalf("ResumeState.BlockingReason = %q, want %q", resumeState.BlockingReason, "approval_required")
	}
	if len(resumeState.NextSteps) == 0 {
		t.Fatalf("ResumeState.NextSteps = %v, want at least one step", resumeState.NextSteps)
	}
	if _, err := latestRunForTask(ctx, store, task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("latestRunForTask() error = %v, want sql.ErrNoRows", err)
	}
}

func TestExecuteNextQueuedRequeuesWhenBlockedWakePacketCompactionFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	compactErr := errors.New("checkpoint write failed")
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
		CheckpointCompactor: func(context.Context, checkpoints.CompactParams) (checkpoints.CompactionResult, error) {
			return checkpoints.CompactionResult{}, compactErr
		},
		Now: func() time.Time { return now },
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeOdinCore,
		ProjectKey: "odin-core",
	}, "Requires approval")
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

	err = service.ExecuteNextQueued(ctx)
	if !errors.Is(err, compactErr) {
		t.Fatalf("ExecuteNextQueued() error = %v, want %v", err, compactErr)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("Task.Status = %q, want queued", gotTask.Status)
	}
	if gotTask.BlockedReason != "" {
		t.Fatalf("BlockedReason = %q, want empty", gotTask.BlockedReason)
	}
	if gotTask.NextEligibleAt != now.Add(time.Second) {
		t.Fatalf("NextEligibleAt = %v, want %v", gotTask.NextEligibleAt, now.Add(time.Second))
	}
	if !strings.Contains(gotTask.LastError, compactErr.Error()) {
		t.Fatalf("LastError = %q, want compaction failure", gotTask.LastError)
	}

	approval, err := store.GetLatestTaskApproval(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskApproval() error = %v", err)
	}
	if approval.Status != "pending" {
		t.Fatalf("Approval.Status = %q, want pending", approval.Status)
	}
	if _, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetLatestTaskWakePacket() error = %v, want sql.ErrNoRows", err)
	}
}

func TestExecuteNextQueuedResumesAfterApprovalIsApproved(t *testing.T) {
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
	}, "Requires approval and resume")
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

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued(first) error = %v", err)
	}

	approval, err := store.GetLatestTaskApproval(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskApproval() error = %v", err)
	}
	if _, err := store.ResolveApproval(ctx, sqlite.ResolveApprovalParams{
		ApprovalID: approval.ID,
		Status:     "approved",
		DecisionBy: "operator",
		Reason:     "safe to resume",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	recordHealthyExecutorSample(t, ctx, store)

	unblockedTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() after approval error = %v", err)
	}
	if unblockedTask.Status != "queued" {
		t.Fatalf("Task.Status after approval = %q, want queued", unblockedTask.Status)
	}
	if unblockedTask.BlockedReason != "" {
		t.Fatalf("BlockedReason after approval = %q, want empty", unblockedTask.BlockedReason)
	}
	if _, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetLatestTaskWakePacket() after approval error = %v, want sql.ErrNoRows", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued(second) error = %v", err)
	}

	completedTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() after resume error = %v", err)
	}
	if completedTask.Status != "completed" {
		t.Fatalf("Task.Status after resume = %q, want completed", completedTask.Status)
	}
}

func TestExecuteNextQueuedKeepsRunPreparingAndReleasesLeaseOnAdmissionFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistryWithAlphaDefaultBranch(t, "odin/alpha/task-1/run-1/try-1")
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
	}, "Admission fails after lease preparation")
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
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   0,
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "failed" {
		t.Fatalf("Task.Status = %q, want failed", gotTask.Status)
	}

	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{RunID: &run.ID})
	if err != nil {
		t.Fatalf("ListEvents(run) error = %v", err)
	}

	runStartStatus := ""
	for _, event := range events {
		if event.Type != runtimeevents.EventRunStarted {
			continue
		}
		payload, err := runtimeevents.DecodePayload[runtimeevents.RunStartedPayload](event.Payload)
		if err != nil {
			t.Fatalf("DecodePayload(run.started) error = %v", err)
		}
		runStartStatus = payload.Status
		break
	}
	if runStartStatus != "preparing" {
		t.Fatalf("run.started status = %q, want preparing", runStartStatus)
	}
	packet, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerHandoff) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerHandoff)
	}
	resumeState, err := checkpoints.Service{Store: store}.LoadResumeState(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.RunContext == nil || resumeState.RunContext.RunID != run.ID {
		t.Fatalf("ResumeState.RunContext = %+v, want run %d", resumeState.RunContext, run.ID)
	}
	if len(resumeState.NextSteps) == 0 {
		t.Fatalf("ResumeState.NextSteps = %v, want at least one step", resumeState.NextSteps)
	}

	if _, err := store.GetActiveWorktreeLeaseByTaskRun(ctx, task.ID, run.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetActiveWorktreeLeaseByTaskRun() error = %v, want sql.ErrNoRows", err)
	}
}

func TestExecuteNextQueuedRequeuesWhenFailedDispatchWakePacketCompactionFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistryWithAlphaDefaultBranch(t, "odin/alpha/task-1/run-1/try-1")
	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	compactErr := errors.New("checkpoint write failed")
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
		CheckpointCompactor: func(context.Context, checkpoints.CompactParams) (checkpoints.CompactionResult, error) {
			return checkpoints.CompactionResult{}, compactErr
		},
		Now: func() time.Time { return now },
	}

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Admission fails after lease preparation")
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
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   0,
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	err = service.ExecuteNextQueued(ctx)
	if !errors.Is(err, compactErr) {
		t.Fatalf("ExecuteNextQueued() error = %v, want %v", err, compactErr)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("Task.Status = %q, want queued", gotTask.Status)
	}
	if gotTask.BlockedReason != "" {
		t.Fatalf("BlockedReason = %q, want empty", gotTask.BlockedReason)
	}
	if gotTask.NextEligibleAt != now.Add(time.Second) {
		t.Fatalf("NextEligibleAt = %v, want %v", gotTask.NextEligibleAt, now.Add(time.Second))
	}
	if !strings.Contains(gotTask.LastError, "dispatch preparation failed") || !strings.Contains(gotTask.LastError, compactErr.Error()) {
		t.Fatalf("LastError = %q, want dispatch and compaction detail", gotTask.LastError)
	}

	run, err := latestRunForTask(ctx, store, task.ID)
	if err != nil {
		t.Fatalf("latestRunForTask() error = %v", err)
	}
	if run.Status != "failed" {
		t.Fatalf("Run.Status = %q, want failed", run.Status)
	}
	if _, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetLatestTaskWakePacket() error = %v, want sql.ErrNoRows", err)
	}
	if _, err := store.GetActiveWorktreeLeaseByTaskRun(ctx, task.ID, run.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetActiveWorktreeLeaseByTaskRun() error = %v, want sql.ErrNoRows", err)
	}
}

func TestRetryBackoffSkipsTaskUntilEligible(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	service := Service{
		Store:    store,
		Registry: registry,
		Now: func() time.Time {
			return now
		},
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	dueTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "due-task",
		Title:       "Eligible now",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(due) error = %v", err)
	}

	delayedTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "delayed-task",
		Title:       "Eligible later",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(delayed) error = %v", err)
	}

	if _, err := store.RequeueTaskAt(ctx, sqlite.RequeueTaskAtParams{
		TaskID:         delayedTask.ID,
		NextEligibleAt: now.Add(500 * time.Millisecond),
	}); err != nil {
		t.Fatalf("RequeueTaskAt() error = %v", err)
	}

	got, err := service.nextQueuedTask(ctx)
	if err != nil {
		t.Fatalf("nextQueuedTask() error = %v", err)
	}
	if got.ID != dueTask.ID {
		t.Fatalf("nextQueuedTask().ID = %d, want %d", got.ID, dueTask.ID)
	}
}

func TestExecuteNextQueuedRequeuesTransientExecutorFailureWithBackoff(t *testing.T) {
	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	service := Service{
		Store:    store,
		Registry: registry,
		Executors: map[string]contract.Executor{
			"codex_headless": transientFailureExecutor{},
		},
		ExecutorConfig: mustLoadExecutorConfig(t),
		Transitions:    projects.Service{Store: store},
		Leases: leases.Manager{
			Store:        store,
			Git:          &jobTestGit{},
			WorktreeRoot: t.TempDir(),
		},
		Now: func() time.Time { return now },
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if _, err := (projects.Service{Store: store}).SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		TargetState: projects.TransitionStateCutover,
		ChangedBy:   "test",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}
	recordHealthyExecutorSample(t, ctx, store)

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "transient-failure",
		Title:       "Retry me",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if err := service.ExecuteNextQueued(ctx); err != nil {
		t.Fatalf("ExecuteNextQueued() error = %v", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("Task.Status = %q, want queued", gotTask.Status)
	}
	if gotTask.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", gotTask.RetryCount)
	}
	if gotTask.LastError != "temporary executor outage" {
		t.Fatalf("LastError = %q, want temporary executor outage", gotTask.LastError)
	}
	if gotTask.NextEligibleAt != now.Add(time.Second) {
		t.Fatalf("NextEligibleAt = %v, want %v", gotTask.NextEligibleAt, now.Add(time.Second))
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

func recordHealthyExecutorSample(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   0,
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
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

	return writeRegistryWithAlphaDefaultBranch(t, "main")
}

func writeRegistryWithAlphaDefaultBranch(t *testing.T, alphaDefaultBranch string) projects.Registry {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "projects.yaml")

	for _, key := range []string{"odin-core", "alpha"} {
		gitRoot := filepath.Join(root, key)
		if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir git root: %v", err)
		}
	}

	configYAML := fmt.Sprintf(`
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
    default_branch: %s
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
`, alphaDefaultBranch)

	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
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

type transientFailureExecutor struct{}

func (transientFailureExecutor) Key() string { return "codex_headless" }

func (transientFailureExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (transientFailureExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{
		Status:    contract.HealthStatusHealthy,
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (transientFailureExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsHeadlessPlan: true,
		TaskKinds: []contract.TaskKind{
			contract.TaskKindGeneral,
		},
		Scopes: []string{"project"},
	}, nil
}

func (transientFailureExecutor) RunTask(context.Context, contract.TaskSpec) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, transientExecutorError{}
}

func (transientFailureExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (transientFailureExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return nil
}

func (transientFailureExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, nil
}

type transientExecutorError struct{}

func (transientExecutorError) Error() string   { return "temporary executor outage" }
func (transientExecutorError) Timeout() bool   { return true }
func (transientExecutorError) Temporary() bool { return true }
