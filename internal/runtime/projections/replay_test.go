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
		RunID:          run.ID,
		Status:         "completed",
		Summary:        "all done",
		TerminalReason: "completed",
		ArtifactsJSON:  `["runs/artifacts/replay.json"]`,
	}); err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}

	if _, err := store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:         task.ID,
		Status:         "completed",
		Summary:        "all done",
		TerminalReason: "completed",
		ArtifactsJSON:  `["runs/artifacts/replay.json"]`,
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
	if replay.Tasks[task.ID].TerminalReason != "completed" {
		t.Fatalf("task replay terminal reason = %q, want %q", replay.Tasks[task.ID].TerminalReason, "completed")
	}
	if replay.Tasks[task.ID].ArtifactsJSON != `["runs/artifacts/replay.json"]` {
		t.Fatalf("task replay artifacts = %q, want persisted artifact pointer", replay.Tasks[task.ID].ArtifactsJSON)
	}

	if replay.Runs[run.ID].Status != "completed" {
		t.Fatalf("run replay status = %q, want %q", replay.Runs[run.ID].Status, "completed")
	}
	if replay.Runs[run.ID].TerminalReason != "completed" {
		t.Fatalf("run replay terminal reason = %q, want %q", replay.Runs[run.ID].TerminalReason, "completed")
	}
	if replay.Runs[run.ID].ArtifactsJSON != `["runs/artifacts/replay.json"]` {
		t.Fatalf("run replay artifacts = %q, want persisted artifact pointer", replay.Runs[run.ID].ArtifactsJSON)
	}

	if replay.Approvals[approval.ID].Status != "approved" {
		t.Fatalf("approval replay status = %q, want %q", replay.Approvals[approval.ID].Status, "approved")
	}
}
