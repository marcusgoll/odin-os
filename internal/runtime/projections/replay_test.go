package projections_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
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

func TestReplayLifecycleCollapsesLegacyDuplicatePendingApprovalsPerTask(t *testing.T) {
	now := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)

	taskCreatedPayload, err := runtimeevents.EncodePayload(runtimeevents.TaskCreatedPayload{
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("EncodePayload(task.created) error = %v", err)
	}
	approvalOnePayload, err := runtimeevents.EncodePayload(runtimeevents.ApprovalRequestedPayload{
		TaskID:      7,
		Status:      "pending",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("EncodePayload(approval.requested#1) error = %v", err)
	}
	approvalTwoPayload, err := runtimeevents.EncodePayload(runtimeevents.ApprovalRequestedPayload{
		TaskID:      7,
		Status:      "pending",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("EncodePayload(approval.requested#2) error = %v", err)
	}

	replay, err := projections.ReplayLifecycle([]runtimeevents.Record{
		{
			StreamType: runtimeevents.StreamTask,
			StreamID:   7,
			Type:       runtimeevents.EventTaskCreated,
			Scope:      "project",
			Payload:    taskCreatedPayload,
			OccurredAt: now,
		},
		{
			StreamType: runtimeevents.StreamApproval,
			StreamID:   101,
			Type:       runtimeevents.EventApprovalRequested,
			Scope:      "project",
			TaskID:     int64Ptr(7),
			Payload:    approvalOnePayload,
			OccurredAt: now.Add(time.Second),
		},
		{
			StreamType: runtimeevents.StreamApproval,
			StreamID:   102,
			Type:       runtimeevents.EventApprovalRequested,
			Scope:      "project",
			TaskID:     int64Ptr(7),
			Payload:    approvalTwoPayload,
			OccurredAt: now.Add(2 * time.Second),
		},
	})
	if err != nil {
		t.Fatalf("ReplayLifecycle() error = %v", err)
	}

	if _, ok := replay.Approvals[101]; ok {
		t.Fatalf("replay.Approvals[101] still present, want collapsed duplicate removed")
	}
	if replay.Approvals[102].Status != "pending" {
		t.Fatalf("replay.Approvals[102].Status = %q, want pending", replay.Approvals[102].Status)
	}
	if replay.Approvals[102].TaskID != 7 {
		t.Fatalf("replay.Approvals[102].TaskID = %d, want 7", replay.Approvals[102].TaskID)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
