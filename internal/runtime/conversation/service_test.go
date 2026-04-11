package conversation_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/runtime/conversation"
	"odin-os/internal/store/sqlite"
)

func TestSnapshotIncludesApprovalsActiveRunsStalledRunsAndProjectTransitions(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	now := time.Now().UTC()
	store.Now = func() time.Time { return now.Add(-2 * time.Hour) }

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

	runningTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "running-task",
		Title:       "Running task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(running) error = %v", err)
	}
	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   runningTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun(running) error = %v", err)
	}

	approvalTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "approval-task",
		Title:       "Approval task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval) error = %v", err)
	}
	approvalRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   approvalTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(approval) error = %v", err)
	}
	if _, _, _, err := store.AwaitApproval(ctx, sqlite.AwaitApprovalParams{
		TaskID:         approvalTask.ID,
		RunID:          approvalRun.ID,
		RequestedBy:    "operator",
		Summary:        "waiting on approval",
		TerminalReason: "waiting on approval",
		ArtifactsJSON:  `[]`,
	}); err != nil {
		t.Fatalf("AwaitApproval() error = %v", err)
	}

	if _, err := store.SetProjectTransition(ctx, sqlite.SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "cutover",
		Controller:         "odin_os",
		LimitedActionsJSON: "[]",
		Notes:              "primary controller",
		ChangedBy:          "operator",
	}); err != nil {
		t.Fatalf("SetProjectTransition() error = %v", err)
	}

	snapshot, err := conversation.Service{
		DB:             store.DB(),
		Now:            func() time.Time { return now },
		StalledTimeout: 30 * time.Minute,
	}.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if len(snapshot.ApprovalsWaiting) != 1 {
		t.Fatalf("approvals waiting = %d, want 1", len(snapshot.ApprovalsWaiting))
	}
	if len(snapshot.ActiveRuns) != 1 {
		t.Fatalf("active runs = %d, want 1", len(snapshot.ActiveRuns))
	}
	if len(snapshot.StalledRuns) != 1 {
		t.Fatalf("stalled runs = %d, want 1", len(snapshot.StalledRuns))
	}
	if len(snapshot.ProjectTransitions) != 1 {
		t.Fatalf("project transitions = %d, want 1", len(snapshot.ProjectTransitions))
	}
	if snapshot.ProjectTransitionOwnership.OdinOS != 1 {
		t.Fatalf("odin_os ownership = %d, want 1", snapshot.ProjectTransitionOwnership.OdinOS)
	}
}
