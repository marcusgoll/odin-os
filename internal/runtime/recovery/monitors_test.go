package recovery_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/runtime/recovery"
	"odin-os/internal/store/sqlite"
)

func TestMonitorDetectsDefinedFaults(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

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
		Key:         "build-app",
		Title:       "Build app",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	runOne, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(first) error = %v", err)
	}
	if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   runOne.ID,
		Status:  "failed",
		Summary: "first failure",
	}); err != nil {
		t.Fatalf("FinishRun(first) error = %v", err)
	}

	runTwo, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  2,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(second) error = %v", err)
	}
	if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   runTwo.ID,
		Status:  "failed",
		Summary: "second failure",
	}); err != nil {
		t.Fatalf("FinishRun(second) error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   25,
		DetailsJSON: `{"mode":"headless"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE executor_health
		SET checked_at = ?
	`, now.Add(-2*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("stale executor update error = %v", err)
	}

	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE projection_freshness
		SET refreshed_at = ?
		WHERE surface = 'doctor'
	`, now.Add(-2*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("stale projection update error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "test version",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE registry_versions
		SET compiled_at = ?
	`, now.Add(-2*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("stale registry update error = %v", err)
	}

	secondTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "qa-app",
		Title:       "QA app",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(second) error = %v", err)
	}
	_ = secondTask

	monitor := recovery.Monitor{
		DB: store.DB(),
		Config: recovery.Config{
			QueuePressureThreshold:      1,
			ExecutorFreshnessTTL:        time.Hour,
			ProjectionFreshnessTTL:      time.Hour,
			SourceFreshnessTTL:          time.Hour,
			RepeatedRunFailureThreshold: 2,
		},
		Now: func() time.Time { return now },
	}

	observations, err := monitor.Observe(ctx)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}

	assertFaultPresent(t, observations, recovery.FaultExecutorHealthStale, "codex_headless")
	assertFaultPresent(t, observations, recovery.FaultProjectionStale, "doctor")
	assertFaultPresent(t, observations, recovery.FaultSourceFreshnessStale, "registry")
	assertFaultPresent(t, observations, recovery.FaultQueuePressureHigh, "task_queue")
	assertFaultPresent(t, observations, recovery.FaultRunFailureRepeated, "task:"+task.Key)
}

func TestMonitorIgnoresDelayedQueuedTasksForQueuePressure(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

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

	runnableTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "build-app",
		Title:       "Build app",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(runnable) error = %v", err)
	}

	for index := 0; index < 3; index++ {
		delayedTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
			ProjectID:   project.ID,
			Key:         "delayed-task-" + string(rune('a'+index)),
			Title:       "Delayed task",
			Status:      "queued",
			Scope:       "project",
			RequestedBy: "operator",
		})
		if err != nil {
			t.Fatalf("CreateTask(delayed %d) error = %v", index, err)
		}
		if _, err := store.RequeueTaskAt(ctx, sqlite.RequeueTaskAtParams{
			TaskID:         delayedTask.ID,
			NextEligibleAt: now.Add(500 * time.Millisecond),
		}); err != nil {
			t.Fatalf("RequeueTaskAt(delayed %d) error = %v", index, err)
		}
	}

	monitor := recovery.Monitor{
		DB: store.DB(),
		Config: recovery.Config{
			QueuePressureThreshold:      1,
			ExecutorFreshnessTTL:        time.Hour,
			ProjectionFreshnessTTL:      time.Hour,
			SourceFreshnessTTL:          time.Hour,
			RepeatedRunFailureThreshold: 2,
		},
		Now: func() time.Time { return now },
	}

	observations, err := monitor.Observe(ctx)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	for _, observation := range observations {
		if observation.FaultKey == recovery.FaultQueuePressureHigh && observation.SubjectKey == "task_queue" {
			t.Fatalf("unexpected queue pressure observation with delayed tasks present: %+v", observations)
		}
	}

	_ = runnableTask
}

func assertFaultPresent(t *testing.T, observations []recovery.Observation, faultKey recovery.FaultKey, subjectKey string) {
	t.Helper()
	for _, observation := range observations {
		if observation.FaultKey == faultKey && observation.SubjectKey == subjectKey {
			return
		}
	}
	t.Fatalf("expected observation %q/%q, got %+v", faultKey, subjectKey, observations)
}
