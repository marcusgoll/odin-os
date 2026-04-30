package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"odin-os/internal/runtime/recovery"
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

func TestCompanionRunCreatesOwnedTaskAndMarksOnlySupportedTriggers(t *testing.T) {
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

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}

	t.Run("supported trigger", func(t *testing.T) {
		task, err := service.CreateTaskFromCompanionRun(ctx, scope.Resolution{
			Kind:       scope.ScopeProject,
			ProjectKey: "alpha",
		}, companion, "review April budget", "build_plus_review")
		if err != nil {
			t.Fatalf("CreateTaskFromCompanionRun() error = %v", err)
		}
		if task.Status != "queued" {
			t.Fatalf("Status = %q, want queued", task.Status)
		}
		if task.WorkspaceID == nil || *task.WorkspaceID != workspace.ID {
			t.Fatalf("WorkspaceID = %v, want %d", task.WorkspaceID, workspace.ID)
		}
		if task.InitiativeID == nil {
			t.Fatal("InitiativeID = nil, want initiative ownership")
		}
		if task.CompanionID == nil || *task.CompanionID != companion.ID {
			t.Fatalf("CompanionID = %v, want %d", task.CompanionID, companion.ID)
		}
		if task.ActionKey != "build_plus_review" {
			t.Fatalf("ActionKey = %q, want build_plus_review", task.ActionKey)
		}
	})

	t.Run("unsupported trigger", func(t *testing.T) {
		task, err := service.CreateTaskFromCompanionRun(ctx, scope.Resolution{
			Kind:       scope.ScopeProject,
			ProjectKey: "alpha",
		}, companion, "review April budget fallback", "single_agent")
		if err != nil {
			t.Fatalf("CreateTaskFromCompanionRun() error = %v", err)
		}
		if task.ActionKey != "" {
			t.Fatalf("ActionKey = %q, want empty when trigger is unsupported", task.ActionKey)
		}
	})
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
		Executors:      testJobExecutors(),
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

func TestExecuteNextQueuedRecordsWorkerPanicAsFailedRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	service := Service{
		Store:    store,
		Registry: registry,
		Executors: map[string]contract.Executor{
			"codex_headless": panicExecutor{},
		},
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
	}, "Panic containment")
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

	err = service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatal("ExecuteNextQueued() error = nil, want recovered worker panic error")
	}
	if !strings.Contains(err.Error(), "worker panic") {
		t.Fatalf("ExecuteNextQueued() error = %v, want worker panic context", err)
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
		t.Fatalf("run.Status = %q, want failed", run.Status)
	}
	if !strings.Contains(run.Summary, "worker panic") {
		t.Fatalf("run.Summary = %q, want worker panic context", run.Summary)
	}
	var artifact struct {
		FailureAnalysis recovery.FailureAnalysis `json:"failure_analysis"`
	}
	if err := json.Unmarshal([]byte(run.ArtifactsJSON), &artifact); err != nil {
		t.Fatalf("json.Unmarshal(run.ArtifactsJSON) error = %v\n%s", err, run.ArtifactsJSON)
	}
	if artifact.FailureAnalysis.Category != recovery.FailureUnknown {
		t.Fatalf("failure category = %q, want unknown", artifact.FailureAnalysis.Category)
	}
	if !artifact.FailureAnalysis.FollowUp.Recommended {
		t.Fatalf("follow-up recommendation = false, want true")
	}
	if artifact.FailureAnalysis.AutoApplyWorkflowChange {
		t.Fatal("AutoApplyWorkflowChange = true, want false")
	}

	if _, err := store.GetActiveWorktreeLeaseByTaskRun(ctx, task.ID, run.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetActiveWorktreeLeaseByTaskRun() error = %v, want sql.ErrNoRows", err)
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
		Executors:      testJobExecutors(),
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
		Executors:      testJobExecutors(),
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
		Executors:      testJobExecutors(),
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

func TestDelegationAdmissionNarrowsChildPermissionsRelativeToParentAndCompanion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 "jobs-workspace",
		Name:                "Jobs Workspace",
		OwnerRef:            "marcus",
		DefaultCompanionKey: "primary",
		Status:              "active",
		PolicyJSON:          `{}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID: workspace.ID,
		Key:         "jobs-initiative",
		Title:       "Jobs Initiative",
		Kind:        "delivery",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}
	companion, err := store.UpsertCompanion(ctx, sqlite.UpsertCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "builder",
		Title:               "Builder",
		Kind:                "assistant",
		Charter:             "Coordinates child work",
		Status:              "active",
		InitiativeScopeJSON: `{"allow":["jobs-initiative"]}`,
		ToolPolicyJSON:      `{"allow":["repo_read","branch_proposal"]}`,
		MemoryPolicyJSON:    `{"mode":"initiative"}`,
		PlanningPolicyJSON:  `{"swarm":{"max_children":2}}`,
	})
	if err != nil {
		t.Fatalf("UpsertCompanion() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "jobs-project",
		Name:          "Jobs Project",
		Scope:         "project",
		GitRoot:       "/tmp/jobs-project",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "parent-task",
		Title:        "Parent task",
		ActionKey:    "execute",
		Status:       "queued",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "project",
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

	service := Service{
		Store:          store,
		Executors:      testJobExecutors(),
		ExecutorConfig: mustLoadExecutorConfig(t),
	}

	profile, err := service.NarrowDelegationAdmission(DelegationAdmissionInput{
		ParentTask:            task,
		ParentRunID:           &run.ID,
		Companion:             companion,
		RequestedTools:        []string{"repo_read", "merge_to_main", "branch_proposal"},
		RequestedMemoryScopes: []string{"workspace", "initiative", "global", "companion", "run"},
		PreferredExecutor:     "",
	})
	if err != nil {
		t.Fatalf("NarrowDelegationAdmission() error = %v", err)
	}

	if profile.Executor != "codex_headless" {
		t.Fatalf("Executor = %q, want codex_headless", profile.Executor)
	}
	wantTools := []string{"repo_read", "branch_proposal"}
	if len(profile.AllowedTools) != len(wantTools) {
		t.Fatalf("AllowedTools len = %d, want %d", len(profile.AllowedTools), len(wantTools))
	}
	for i, want := range wantTools {
		if profile.AllowedTools[i] != want {
			t.Fatalf("AllowedTools[%d] = %q, want %q", i, profile.AllowedTools[i], want)
		}
	}
	if profile.MemoryView.Mode != "initiative" {
		t.Fatalf("MemoryView.Mode = %q, want initiative", profile.MemoryView.Mode)
	}
	wantScopes := []string{"workspace", "initiative", "companion", "run"}
	if len(profile.MemoryView.Scopes) != len(wantScopes) {
		t.Fatalf("MemoryView.Scopes len = %d, want %d", len(profile.MemoryView.Scopes), len(wantScopes))
	}
	for i, want := range wantScopes {
		if profile.MemoryView.Scopes[i] != want {
			t.Fatalf("MemoryView.Scopes[%d] = %q, want %q", i, profile.MemoryView.Scopes[i], want)
		}
	}
	if profile.MemoryView.ParentRunID == nil || *profile.MemoryView.ParentRunID != run.ID {
		t.Fatalf("MemoryView.ParentRunID = %v, want %d", profile.MemoryView.ParentRunID, run.ID)
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
		Executors:      testJobExecutors(),
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
		Executors:      testJobExecutors(),
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
		Executors:      testJobExecutors(),
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
		Executors:      testJobExecutors(),
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
		Executors:      testJobExecutors(),
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
		Executors:      testJobExecutors(),
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
		Executors:      testJobExecutors(),
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

func TestExecutionMetadataForResultKeepsOdinReservedFieldsAuthoritative(t *testing.T) {
	t.Parallel()

	metadata := executionMetadataForResult(
		map[string]string{
			"operator_note": "keep",
			"executor_lane": "request_lane",
			"repo_root":     "/request/repo",
			"worktree_path": "/request/worktree",
			"branch_name":   "request-branch",
		},
		map[string]string{
			"driver_kind":    "fixture",
			"external_id":    "driver-id",
			"marker_written": "true",
			"repo_root":      "/driver/repo",
			"worktree_path":  "/driver/worktree",
			"branch_name":    "driver-branch",
			"unallowlisted":  "drop",
		},
		leases.Assignment{
			RepoRoot:     "/odin/repo",
			WorktreePath: "/odin/worktree",
			BranchName:   "odin/task-1",
		},
		"sandcastle_headless",
		"handle-id",
	)

	for key, want := range map[string]string{
		"operator_note":  "keep",
		"driver_kind":    "fixture",
		"external_id":    "handle-id",
		"marker_written": "true",
		"executor_lane":  "sandcastle_headless",
		"repo_root":      "/odin/repo",
		"worktree_path":  "/odin/worktree",
		"branch_name":    "odin/task-1",
	} {
		if got := metadata[key]; got != want {
			t.Fatalf("metadata[%s] = %q, want %q in %#v", key, got, want, metadata)
		}
	}
	if _, ok := metadata["unallowlisted"]; ok {
		t.Fatalf("metadata included unallowlisted driver field: %#v", metadata)
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

func testJobExecutors() map[string]contract.Executor {
	return map[string]contract.Executor{
		"codex_headless": jobTestExecutor{
			key: "codex_headless",
			result: contract.ExecutionResult{
				Status: "completed",
				Output: "task complete",
			},
		},
	}
}

type jobTestExecutor struct {
	key    string
	result contract.ExecutionResult
}

func (executor jobTestExecutor) Key() string { return executor.key }

func (jobTestExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (jobTestExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{
		Status:    contract.HealthStatusHealthy,
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (jobTestExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
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

func (executor jobTestExecutor) RunTask(context.Context, contract.TaskSpec) (contract.ExecutionResult, error) {
	return executor.result, nil
}

func (jobTestExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (jobTestExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (jobTestExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}

type panicExecutor struct{}

func (panicExecutor) Key() string { return "codex_headless" }

func (panicExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (panicExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{
		Status:    contract.HealthStatusHealthy,
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (panicExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsHeadlessPlan: true,
		TaskKinds: []contract.TaskKind{
			contract.TaskKindGeneral,
		},
		Scopes: []string{"project"},
	}, nil
}

func (panicExecutor) RunTask(context.Context, contract.TaskSpec) (contract.ExecutionResult, error) {
	panic("simulated worker panic")
}

func (panicExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (panicExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return nil
}

func (panicExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, nil
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
