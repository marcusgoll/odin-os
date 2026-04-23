package projections_test

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestReplayLifecycleBuildsCurrentStateFromEvents(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "phase-03",
		Title:       "Implement runtime store",
		Status:      "queued",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if _, err := store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "running",
	}); err != nil {
		t.Fatalf("UpdateTaskStatus(running) error = %v", err)
	}

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   run.ID,
		Status:  "completed",
		Summary: "all done",
	}); err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}

	if _, err := store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "completed",
	}); err != nil {
		t.Fatalf("UpdateTaskStatus(completed) error = %v", err)
	}

	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	if _, err := store.ResolveApproval(ctx, sqlite.ResolveApprovalParams{
		ApprovalID: approval.ID,
		Status:     "approved",
		DecisionBy: "operator",
		Reason:     "safe",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	records, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	replay, err := projections.ReplayLifecycle(records)
	if err != nil {
		t.Fatalf("ReplayLifecycle() error = %v", err)
	}

	if replay.Tasks[task.ID].Status != "completed" {
		t.Fatalf("task replay status = %q, want %q", replay.Tasks[task.ID].Status, "completed")
	}

	if replay.Runs[run.ID].Status != "completed" {
		t.Fatalf("run replay status = %q, want %q", replay.Runs[run.ID].Status, "completed")
	}

	if replay.Approvals[approval.ID].Status != "approved" {
		t.Fatalf("approval replay status = %q, want %q", replay.Approvals[approval.ID].Status, "approved")
	}
}

func TestReplayLifecycleIncludesDefaultQueueStateFromTaskCreated(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

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
		Title:       "Alpha Task",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	records, err := store.ListEvents(ctx, sqlite.ListEventsParams{TaskID: &task.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	replay, err := projections.ReplayLifecycle(records)
	if err != nil {
		t.Fatalf("ReplayLifecycle() error = %v", err)
	}

	got := replay.Tasks[task.ID]
	if got.NextEligibleAt != "0001-01-01T00:00:00.000000000Z" {
		t.Fatalf("task replay next_eligible_at = %q, want zero timestamp", got.NextEligibleAt)
	}
	if got.Priority != 100 {
		t.Fatalf("task replay priority = %d, want 100", got.Priority)
	}
	if got.RetryCount != 0 {
		t.Fatalf("task replay retry_count = %d, want 0", got.RetryCount)
	}
	if got.MaxAttempts != 3 {
		t.Fatalf("task replay max_attempts = %d, want 3", got.MaxAttempts)
	}
	if got.LastError != "" {
		t.Fatalf("task replay last_error = %q, want empty", got.LastError)
	}
	if got.BlockedReason != "" {
		t.Fatalf("task replay blocked_reason = %q, want empty", got.BlockedReason)
	}
}
