package recovery_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/runtime/checkpoints"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/store/sqlite"
)

func TestRunStartupRecoveryInterruptsRunsAndCreatesWakePackets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 9, 21, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	store.Now = func() time.Time { return now }

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
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

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Resume alpha work",
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

	service := recovery.Service{
		Store: store,
		Now:   func() time.Time { return now },
	}

	result, err := service.RunStartupRecovery(ctx)
	if err != nil {
		t.Fatalf("RunStartupRecovery() error = %v", err)
	}
	if result.RecoveredRuns != 1 {
		t.Fatalf("RecoveredRuns = %d, want 1", result.RecoveredRuns)
	}

	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "interrupted" {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, "interrupted")
	}
	if gotRun.FinishedAt == nil {
		t.Fatalf("GetRun().FinishedAt = nil, want timestamp")
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "queued" {
		t.Fatalf("GetTask().Status = %q, want %q", gotTask.Status, "queued")
	}
	if gotTask.CurrentRunID != nil {
		t.Fatalf("GetTask().CurrentRunID = %v, want nil", gotTask.CurrentRunID)
	}

	packet, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if packet.Trigger != string(checkpoints.TriggerRestart) {
		t.Fatalf("WakePacket.Trigger = %q, want %q", packet.Trigger, checkpoints.TriggerRestart)
	}

	resumeState, err := checkpoints.Service{Store: store}.LoadResumeState(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}
	if resumeState.Status != "queued" {
		t.Fatalf("ResumeState.Status = %q, want %q", resumeState.Status, "queued")
	}
	if len(resumeState.NextSteps) == 0 {
		t.Fatalf("ResumeState.NextSteps = %v, want at least one step", resumeState.NextSteps)
	}

	recoveries, err := projections.ListRecoveryViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListRecoveryViews() error = %v", err)
	}
	if len(recoveries) != 1 || recoveries[0].Status != "completed" {
		t.Fatalf("recoveries = %+v, want one completed recovery", recoveries)
	}
}
