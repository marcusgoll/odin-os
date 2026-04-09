package recovery_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/store/sqlite"
)

func TestExecutorSuppressesRetriesDuringCooldown(t *testing.T) {
	ctx := context.Background()
	store, projectID, taskID, runID := setupRecoveryFixture(t, ctx)

	now := time.Date(2026, 4, 9, 14, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	executor := recovery.Executor{
		Store: store,
		Playbooks: map[string]recovery.Playbook{
			"refresh_projection_freshness": {
				Name:          "refresh_projection_freshness",
				FaultKey:      recovery.FaultProjectionStale,
				AllowedScopes: []string{"global", "project", "odin-core"},
				MaxRetries:    3,
				Cooldown:      time.Hour,
				ActionName:    "refresh_projection_surface",
				Action: func(context.Context, recovery.ActionContext) (recovery.ActionResult, error) {
					return recovery.ActionResult{Status: "failed", Description: "projection is still stale"}, nil
				},
			},
		},
		Now: func() time.Time { return now },
	}

	decision := recovery.Decision{
		Observation: recovery.Observation{
			FaultKey:   recovery.FaultProjectionStale,
			SubjectKey: "doctor",
			Scope:      "global",
			ProjectID:  &projectID,
			TaskID:     &taskID,
			RunID:      &runID,
		},
		Playbook: "refresh_projection_freshness",
	}

	first, err := executor.Execute(ctx, decision)
	if err != nil {
		t.Fatalf("Execute(first) error = %v", err)
	}
	if first.Status != "failed" {
		t.Fatalf("first.Status = %q, want failed", first.Status)
	}

	second, err := executor.Execute(ctx, decision)
	if err != nil {
		t.Fatalf("Execute(second) error = %v", err)
	}
	if second.Status != "suppressed" || !second.Suppressed {
		t.Fatalf("second outcome = %+v, want suppressed cooldown result", second)
	}

	var recoveryCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM recoveries`).Scan(&recoveryCount); err != nil {
		t.Fatalf("recoveries count query error = %v", err)
	}
	if recoveryCount != 1 {
		t.Fatalf("recoveries count = %d, want 1", recoveryCount)
	}
}

func TestExecutorEscalatesAfterRetryLimit(t *testing.T) {
	ctx := context.Background()
	store, projectID, taskID, runID := setupRecoveryFixture(t, ctx)

	now := time.Date(2026, 4, 9, 15, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	executor := recovery.Executor{
		Store: store,
		Playbooks: map[string]recovery.Playbook{
			"checkpoint_failed_run": {
				Name:          "checkpoint_failed_run",
				FaultKey:      recovery.FaultRunFailureRepeated,
				AllowedScopes: []string{"project", "odin-core"},
				MaxRetries:    2,
				Cooldown:      time.Minute,
				ActionName:    "write_task_wake_packet",
				Action: func(context.Context, recovery.ActionContext) (recovery.ActionResult, error) {
					return recovery.ActionResult{Status: "failed", Description: "task checkpoint did not clear the fault"}, nil
				},
			},
		},
		Now: func() time.Time { return now },
	}

	decision := recovery.Decision{
		Observation: recovery.Observation{
			FaultKey:   recovery.FaultRunFailureRepeated,
			SubjectKey: "task:demo-task",
			Scope:      "project",
			ProjectID:  &projectID,
			TaskID:     &taskID,
			RunID:      &runID,
		},
		Playbook: "checkpoint_failed_run",
	}

	first, err := executor.Execute(ctx, decision)
	if err != nil {
		t.Fatalf("Execute(first) error = %v", err)
	}
	if first.Status != "failed" {
		t.Fatalf("first.Status = %q, want failed", first.Status)
	}

	now = now.Add(2 * time.Minute)

	second, err := executor.Execute(ctx, decision)
	if err != nil {
		t.Fatalf("Execute(second) error = %v", err)
	}
	if second.Status != "escalated" || !second.Escalated {
		t.Fatalf("second outcome = %+v, want escalated result", second)
	}

	incident, err := store.GetIncident(ctx, second.Incident.ID)
	if err != nil {
		t.Fatalf("GetIncident() error = %v", err)
	}
	if incident.Status != "escalated" {
		t.Fatalf("incident.Status = %q, want escalated", incident.Status)
	}
}

func TestExecutorRecordsRecoveryActionWhenPlaybookSucceeds(t *testing.T) {
	ctx := context.Background()
	store, projectID, taskID, runID := setupRecoveryFixture(t, ctx)

	now := time.Date(2026, 4, 9, 16, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	executor := recovery.Executor{
		Store: store,
		Playbooks: map[string]recovery.Playbook{
			"reload_registry_source": {
				Name:          "reload_registry_source",
				FaultKey:      recovery.FaultSourceFreshnessStale,
				AllowedScopes: []string{"global", "project", "odin-core"},
				MaxRetries:    1,
				Cooldown:      time.Minute,
				ActionName:    "reload_registry_snapshot",
				Action: func(context.Context, recovery.ActionContext) (recovery.ActionResult, error) {
					return recovery.ActionResult{Status: "completed", Description: "registry snapshot reloaded"}, nil
				},
			},
		},
		Now: func() time.Time { return now },
	}

	decision := recovery.Decision{
		Observation: recovery.Observation{
			FaultKey:   recovery.FaultSourceFreshnessStale,
			SubjectKey: "registry",
			Scope:      "global",
			ProjectID:  &projectID,
			TaskID:     &taskID,
			RunID:      &runID,
		},
		Playbook: "reload_registry_source",
	}

	outcome, err := executor.Execute(ctx, decision)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if outcome.Status != "completed" {
		t.Fatalf("outcome.Status = %q, want completed", outcome.Status)
	}

	incident, err := store.GetIncident(ctx, outcome.Incident.ID)
	if err != nil {
		t.Fatalf("GetIncident() error = %v", err)
	}
	if incident.Status != "resolved" {
		t.Fatalf("incident.Status = %q, want resolved", incident.Status)
	}

	recoveryRecord, err := store.GetRecovery(ctx, outcome.Recovery.ID)
	if err != nil {
		t.Fatalf("GetRecovery() error = %v", err)
	}
	if recoveryRecord.Status != "completed" {
		t.Fatalf("recovery.Status = %q, want completed", recoveryRecord.Status)
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{RunID: &runID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var sawAction bool
	for _, event := range events {
		if event.Type != runtimeevents.EventRecoveryActionExecuted {
			continue
		}
		sawAction = true
	}
	if !sawAction {
		t.Fatalf("expected recovery action event, got %+v", events)
	}
}

func TestExecutorRejectsUnknownPlaybooks(t *testing.T) {
	ctx := context.Background()
	store, projectID, taskID, runID := setupRecoveryFixture(t, ctx)

	executor := recovery.Executor{
		Store:     store,
		Playbooks: map[string]recovery.Playbook{},
	}

	_, err := executor.Execute(ctx, recovery.Decision{
		Observation: recovery.Observation{
			FaultKey:   recovery.FaultProjectionStale,
			SubjectKey: "doctor",
			Scope:      "global",
			ProjectID:  &projectID,
			TaskID:     &taskID,
			RunID:      &runID,
		},
		Playbook: "missing",
	})
	if !errors.Is(err, recovery.ErrUnknownPlaybook) {
		t.Fatalf("Execute() error = %v, want ErrUnknownPlaybook", err)
	}
}

func setupRecoveryFixture(t *testing.T, ctx context.Context) (*sqlite.Store, int64, int64, int64) {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "demo",
		Name:          "Demo",
		Scope:         "project",
		GitRoot:       "/tmp/demo",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "demo-task",
		Title:       "Demo task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	return store, project.ID, task.ID, run.ID
}
