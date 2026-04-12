package jobs

import (
	"context"
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

func TestCreateTaskFromActStoresExplicitActionKey(t *testing.T) {
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

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "action:docs_audit_note Create docs audit note for limited action pilot")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	if task.ActionKey != "docs_audit_note" {
		t.Fatalf("task.ActionKey = %q, want docs_audit_note", task.ActionKey)
	}
	if task.Title != "Create docs audit note for limited action pilot" {
		t.Fatalf("task.Title = %q, want trimmed title", task.Title)
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

func TestCreateTaskFromProjectKeyUsesExplicitProject(t *testing.T) {
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

	task, err := service.CreateTaskFromProjectKey(ctx, "alpha", "CLI explicit project")
	if err != nil {
		t.Fatalf("CreateTaskFromProjectKey() error = %v", err)
	}
	if task.Status != "queued" || task.Scope != string(scope.ScopeProject) {
		t.Fatalf("task = %+v, want queued project task", task)
	}
}

func TestExecuteNextQueuedCompletesCutoverProjectTask(t *testing.T) {
	configureHarnessDriver(t)

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

func TestExecuteTaskFailsCleanlyWithoutHarnessDriver(t *testing.T) {
	ctx := context.Background()
	t.Setenv("ODIN_CODEX_DRIVER", filepath.Join(t.TempDir(), "missing-driver.sh"))
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
	}, "No harness driver configured")
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

	outcome, err := service.ExecuteTask(ctx, task.ID)
	if err == nil {
		t.Fatal("ExecuteTask() error = nil, want harness driver failure")
	}
	if !strings.Contains(err.Error(), "no harness driver configured") {
		t.Fatalf("error = %v, want no harness driver configured", err)
	}
	if outcome.Run != nil {
		t.Fatalf("Run = %+v, want nil because execution never started", outcome.Run)
	}
	if outcome.Task.Status != "failed" {
		t.Fatalf("Task.Status = %q, want failed", outcome.Task.Status)
	}
}

func TestExecuteNextQueuedRejectsShadowModeMutation(t *testing.T) {
	configureHarnessDriver(t)

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

func TestExecuteNextQueuedRejectsExplicitBoundedActionOnPromotionLine(t *testing.T) {
	configureHarnessDriver(t)

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
	}, "action:docs_audit_note Create docs audit note for limited action pilot")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:      project.ID,
		Actor:          projects.TransitionControllerOdinOS,
		TargetState:    projects.TransitionStateLimitedAction,
		LimitedActions: []string{"docs_audit_note"},
		ChangedBy:      "test",
		Notes:          "infra only",
	}); err != nil {
		t.Fatalf("SetTransitionState(limited_action) error = %v", err)
	}

	err = service.ExecuteNextQueued(ctx)
	if err == nil {
		t.Fatalf("ExecuteNextQueued() error = nil, want fail-closed bounded action denial")
	}
	if !strings.Contains(err.Error(), "not enabled on this line") {
		t.Fatalf("ExecuteNextQueued() error = %v, want line-disabled denial", err)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "failed" {
		t.Fatalf("GetTask().Status = %q, want failed", gotTask.Status)
	}

	row := store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM worktree_leases
		WHERE task_id = ?
	`, task.ID)
	var leaseCount int
	if err := row.Scan(&leaseCount); err != nil {
		t.Fatalf("Scan(leaseCount) error = %v", err)
	}
	if leaseCount != 0 {
		t.Fatalf("leaseCount = %d, want 0", leaseCount)
	}
}

func TestForegroundActExecutionPersistsTranscriptAndEpisode(t *testing.T) {
	configureHarnessDriver(t)

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
	}, "Persist act transcript")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	if _, err := service.Transitions.SetTransitionState(ctx, projects.TransitionStateInput{
		ProjectID:      project.ID,
		Actor:          projects.TransitionControllerOdinOS,
		TargetState:    projects.TransitionStateLimitedAction,
		LimitedActions: []string{"run_task"},
		ChangedBy:      "test",
		Notes:          "allow foreground execution",
	}); err != nil {
		t.Fatalf("SetTransitionState(limited_action) error = %v", err)
	}

	outcome, err := service.ExecuteTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	if outcome.Run == nil {
		t.Fatal("Run = nil, want completed run")
	}

	transcripts, err := store.ListConversationTranscripts(ctx, sqlite.ListConversationTranscriptsParams{
		ProjectID: &project.ID,
		TaskID:    &task.ID,
		RunID:     &outcome.Run.ID,
		Scope:     "project",
		ScopeKey:  project.Key,
		Mode:      "act",
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts() error = %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("transcripts len = %d, want 1", len(transcripts))
	}

	summaries, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		ProjectID:  &project.ID,
		TaskID:     &task.ID,
		RunID:      &outcome.Run.ID,
		Scope:      "project",
		ScopeKey:   project.Key,
		MemoryType: "episode",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
	}
}

func TestForegroundActDenialPersistsTranscriptAndEpisode(t *testing.T) {
	configureHarnessDriver(t)

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
	}, "Persist denied act transcript")
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

	outcome, err := service.ExecuteTask(ctx, task.ID)
	if err == nil {
		t.Fatalf("ExecuteTask() error = nil, want transition denial")
	}
	if outcome.Run == nil {
		t.Fatal("Run = nil, want failed run record")
	}

	transcripts, err := store.ListConversationTranscripts(ctx, sqlite.ListConversationTranscriptsParams{
		ProjectID: &project.ID,
		TaskID:    &task.ID,
		RunID:     &outcome.Run.ID,
		Scope:     "project",
		ScopeKey:  project.Key,
		Mode:      "act",
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts() error = %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("transcripts len = %d, want 1", len(transcripts))
	}
	if transcripts[0].Response == "" {
		t.Fatalf("Response = empty, want denial summary")
	}

	summaries, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		ProjectID:  &project.ID,
		TaskID:     &task.ID,
		RunID:      &outcome.Run.ID,
		Scope:      "project",
		ScopeKey:   project.Key,
		MemoryType: "episode",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
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
		RuntimeRoot: t.TempDir(),
		Now:         time.Now,
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
		RuntimeRoot: t.TempDir(),
		Now:         time.Now,
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
	if captured.Metadata["runtime_root"] == "" {
		t.Fatal("runtime_root metadata empty, want durable artifact root passed to executor")
	}
}

type jobTestGit struct{}

func (jobTestGit) BranchExists(context.Context, string, string) (bool, error) { return false, nil }
func (jobTestGit) CreateBranch(context.Context, string, string, string) error { return nil }
func (jobTestGit) AddWorktree(context.Context, string, string, string) error  { return nil }
func (jobTestGit) RemoveWorktree(context.Context, string, string) error       { return nil }

func configureHarnessDriver(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "codex-driver.sh")
	if err := os.WriteFile(path, []byte(`#!/usr/bin/env bash
cat >/dev/null
printf '{"status":"completed","output":"driver test ok","external_id":"fixture-driver"}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	t.Setenv("ODIN_CODEX_DRIVER", path)
}

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
      limited_actions:
        docs_audit_note:
          description: Create an additive audit note under docs/audits
          path_prefixes: [docs/audits/]
          content_mode: create_markdown_note
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
      limited_actions:
        docs_audit_note:
          description: Create an additive audit note under docs/audits
          path_prefixes: [docs/audits/]
          content_mode: create_markdown_note
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
