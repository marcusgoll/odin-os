package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/router"
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

func TestCreateTaskHonorsRequestedBy(t *testing.T) {
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

	task, err := service.CreateTask(ctx, CreateTaskParams{
		Resolved: scope.Resolution{
			Kind:       scope.ScopeProject,
			ProjectKey: "alpha",
		},
		Title:       "Delegated dashboard audit",
		RequestedBy: "agent:portal-delivery-agent",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.RequestedBy != "agent:portal-delivery-agent" {
		t.Fatalf("task.RequestedBy = %q, want agent:portal-delivery-agent", task.RequestedBy)
	}
}

func TestCreateTaskAvoidsKeyCollisionsWithinSameSecond(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	fixedNow := time.Date(2026, 4, 20, 3, 0, 0, 0, time.UTC)
	service := Service{
		Store:    store,
		Registry: registry,
		Now: func() time.Time {
			return fixedNow
		},
	}

	first, err := service.CreateTask(ctx, CreateTaskParams{
		Resolved: scope.Resolution{
			Kind:       scope.ScopeProject,
			ProjectKey: "alpha",
		},
		Title:       "Delegated dashboard audit",
		RequestedBy: "agent:portal-delivery-agent",
	})
	if err != nil {
		t.Fatalf("CreateTask(first) error = %v", err)
	}
	second, err := service.CreateTask(ctx, CreateTaskParams{
		Resolved: scope.Resolution{
			Kind:       scope.ScopeProject,
			ProjectKey: "alpha",
		},
		Title:       "Delegated dashboard audit",
		RequestedBy: "agent:portal-delivery-agent",
	})
	if err != nil {
		t.Fatalf("CreateTask(second) error = %v", err)
	}
	if first.Key == second.Key {
		t.Fatalf("task keys collided: %q", first.Key)
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

func TestCancelTaskByKeyCancelsQueuedTask(t *testing.T) {
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

	task, err := service.CreateTaskFromAct(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, "Queued task")
	if err != nil {
		t.Fatalf("CreateTaskFromAct() error = %v", err)
	}

	view, err := service.CancelTaskByKey(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, task.Key)
	if err != nil {
		t.Fatalf("CancelTaskByKey() error = %v", err)
	}
	if view.Status != "cancelled" {
		t.Fatalf("view.Status = %q, want cancelled", view.Status)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "cancelled" {
		t.Fatalf("GetTask().Status = %q, want cancelled", gotTask.Status)
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

func TestExecuteNextQueuedUsesConfiguredCodexDriver(t *testing.T) {
	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	driverPath := filepath.Join(t.TempDir(), "driver.sh")
	if err := os.WriteFile(driverPath, []byte(`#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null
printf '{"status":"completed","output":"family-ops triage summary","external_id":"driver-run-1","metadata":{"lane":"driver"}}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	t.Setenv("ODIN_CODEX_DRIVER", driverPath)

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
	}, "Driver-backed task")
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
		Notes:          "allow driver-backed execution",
	}); err != nil {
		t.Fatalf("SetTransitionState(limited_action) error = %v", err)
	}

	outcome, err := service.ExecuteTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	if outcome.Run == nil {
		t.Fatal("Run = nil, want driver-backed run")
	}
	if outcome.Run.Summary != "family-ops triage summary" {
		t.Fatalf("Run.Summary = %q, want driver-backed summary", outcome.Run.Summary)
	}
	if outcome.Run.Executor != "codex_headless" {
		t.Fatalf("Run.Executor = %q, want codex_headless", outcome.Run.Executor)
	}
}

func TestExecuteTaskWithRequestPersistsSkillTelemetry(t *testing.T) {
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

	task, err := service.CreateTask(ctx, CreateTaskParams{
		Resolved: scope.Resolution{
			Kind:       scope.ScopeProject,
			ProjectKey: "alpha",
		},
		Title:       "Portal design child",
		RequestedBy: "agent:portal-delivery-agent",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
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

	outcome, err := service.ExecuteTaskWithRequest(ctx, task.ID, ExecutionRequest{
		PromptOverride: "Produce a dashboard design direction.",
		Metadata: map[string]string{
			"agent_key":       "portal-delivery-agent",
			"delegation_id":   "42",
			"portal_track":    "admin-cfi",
			"requested_skill": "pixel-perfect-ui-ux-designer",
			"effective_skill": "pixel-perfect-ui-ux-designer",
			"skill_source":    "agent_template",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTaskWithRequest() error = %v", err)
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
	var toolSummary map[string]string
	if err := json.Unmarshal([]byte(transcripts[0].ToolSummary), &toolSummary); err != nil {
		t.Fatalf("json.Unmarshal(tool summary) error = %v", err)
	}
	for key, want := range map[string]string{
		"agent_key":       "portal-delivery-agent",
		"delegation_id":   "42",
		"portal_track":    "admin-cfi",
		"requested_skill": "pixel-perfect-ui-ux-designer",
		"effective_skill": "pixel-perfect-ui-ux-designer",
		"skill_source":    "agent_template",
	} {
		if got := toolSummary[key]; got != want {
			t.Fatalf("toolSummary[%q] = %q, want %q", key, got, want)
		}
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
	var details map[string]any
	if err := json.Unmarshal([]byte(summaries[0].DetailsJSON), &details); err != nil {
		t.Fatalf("json.Unmarshal(details) error = %v", err)
	}
	executionMetadata, ok := details["execution_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("execution_metadata = %#v, want object", details["execution_metadata"])
	}
	for key, want := range map[string]string{
		"agent_key":       "portal-delivery-agent",
		"delegation_id":   "42",
		"portal_track":    "admin-cfi",
		"requested_skill": "pixel-perfect-ui-ux-designer",
		"effective_skill": "pixel-perfect-ui-ux-designer",
		"skill_source":    "agent_template",
	} {
		if got, _ := executionMetadata[key].(string); got != want {
			t.Fatalf("execution_metadata[%q] = %q, want %q", key, got, want)
		}
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

func TestForegroundActExecutionPersistsTranscriptAndEpisode(t *testing.T) {
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

func TestExecuteTaskPreservesCancelledRunStatus(t *testing.T) {
	ctx := context.Background()
	store := openJobStore(t)
	defer store.Close()

	registry := writeRegistry(t)
	blocking := newBlockingExecutor("blocking")
	service := Service{
		Store:    store,
		Registry: registry,
		Executors: map[string]contract.Executor{
			"blocking": blocking,
		},
		ExecutorConfig: router.Config{
			Version: 1,
			Executors: []router.ExecutorConfig{{
				Key:     "blocking",
				Adapter: "blocking",
				Class:   contract.ExecutorClassPlanBackedCLI,
				Enabled: true,
			}},
			Routes: []router.RouteConfig{{
				Name:      "default",
				Preferred: []string{"blocking"},
				Match: router.RouteMatch{
					TaskKinds: []contract.TaskKind{contract.TaskKindGeneral},
					Scopes:    []string{"project"},
				},
			}},
		},
		Transitions: projects.Service{Store: store},
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
	}, "Blocking task")
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
		Notes:          "allow blocking execution",
	}); err != nil {
		t.Fatalf("SetTransitionState(limited_action) error = %v", err)
	}

	outcomeCh := make(chan ExecutionOutcome, 1)
	errCh := make(chan error, 1)
	go func() {
		outcome, execErr := service.ExecuteTask(ctx, task.ID)
		outcomeCh <- outcome
		errCh <- execErr
	}()

	runID := waitForRunningTaskRun(t, ctx, store, task.ID)
	cancelDetail, err := runsvc.Service{
		DB:    store.DB(),
		Store: store,
	}.Cancel(ctx, scope.Resolution{
		Kind:       scope.ScopeProject,
		ProjectKey: "alpha",
	}, runID)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if cancelDetail.Run.Status != "cancelled" {
		t.Fatalf("cancel detail run status = %q, want cancelled", cancelDetail.Run.Status)
	}

	blocking.Unblock(contract.ExecutionResult{
		Status: "completed",
		Output: "executor completed after cancellation",
	}, nil)

	outcome := <-outcomeCh
	execErr := <-errCh
	if execErr != nil {
		t.Fatalf("ExecuteTask() error = %v", execErr)
	}
	if outcome.Run == nil {
		t.Fatal("Run = nil, want cancelled run")
	}
	if outcome.Run.Status != "cancelled" {
		t.Fatalf("Run.Status = %q, want cancelled", outcome.Run.Status)
	}

	gotRun, err := store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "cancelled" {
		t.Fatalf("GetRun().Status = %q, want cancelled", gotRun.Status)
	}
	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "cancelled" {
		t.Fatalf("GetTask().Status = %q, want cancelled", gotTask.Status)
	}

	transcripts, err := store.ListConversationTranscripts(ctx, sqlite.ListConversationTranscriptsParams{
		ProjectID: &project.ID,
		TaskID:    &task.ID,
		RunID:     &runID,
		Scope:     "project",
		ScopeKey:  project.Key,
		Mode:      "act",
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts() error = %v", err)
	}
	if len(transcripts) != 0 {
		t.Fatalf("transcripts = %+v, want none for cancelled run", transcripts)
	}

	summaries, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		ProjectID:  &project.ID,
		TaskID:     &task.ID,
		RunID:      &runID,
		Scope:      "project",
		ScopeKey:   project.Key,
		MemoryType: "episode",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries() error = %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("summaries = %+v, want none for cancelled run", summaries)
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

func waitForRunningTaskRun(t *testing.T, ctx context.Context, store *sqlite.Store, taskID int64) int64 {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := store.GetTask(ctx, taskID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if task.CurrentRunID != nil {
			run, err := store.GetRun(ctx, *task.CurrentRunID)
			if err != nil {
				t.Fatalf("GetRun() error = %v", err)
			}
			if run.Status == "running" {
				return run.ID
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for running task run")
	return 0
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

type blockingExecutor struct {
	key     string
	entered chan struct{}
	done    chan blockingResult
	once    sync.Once
}

type blockingResult struct {
	result contract.ExecutionResult
	err    error
}

func newBlockingExecutor(key string) *blockingExecutor {
	return &blockingExecutor{
		key:     key,
		entered: make(chan struct{}),
		done:    make(chan blockingResult, 1),
	}
}

func (executor *blockingExecutor) Key() string { return executor.key }

func (executor *blockingExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (executor *blockingExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{Status: contract.HealthStatusHealthy}, nil
}

func (executor *blockingExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsHeadlessPlan: true,
		TaskKinds:            []contract.TaskKind{contract.TaskKindGeneral},
		Scopes:               []string{"project"},
	}, nil
}

func (executor *blockingExecutor) RunTask(context.Context, contract.TaskSpec) (contract.ExecutionResult, error) {
	executor.once.Do(func() { close(executor.entered) })
	result := <-executor.done
	return result.result, result.err
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

func (executor *blockingExecutor) Unblock(result contract.ExecutionResult, err error) {
	executor.done <- blockingResult{result: result, err: err}
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
