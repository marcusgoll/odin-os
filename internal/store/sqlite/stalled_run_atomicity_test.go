package sqlite

import (
	"context"
	"testing"
)

func TestResolveStalledRunRollsBackWhenLeaseReleaseFails(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "resolve-stalled-run.db")
	defer store.Close()

	project, task, run := seedContextPacketTask(t, ctx, store)

	lease, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: "/tmp/odin/cfipros/task-1/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		CREATE TRIGGER fail_release_worktree_lease
		BEFORE UPDATE ON worktree_leases
		WHEN NEW.state = 'released'
		BEGIN
			SELECT RAISE(FAIL, 'lease release blocked');
		END;
	`); err != nil {
		t.Fatalf("create trigger error = %v", err)
	}

	err = store.ResolveStalledRun(ctx, ResolveStalledRunParams{
		RunID:          run.ID,
		TaskID:         task.ID,
		TaskStatus:     "dead_letter",
		Summary:        "stalled run retry budget exhausted",
		TerminalReason: "stalled run retry budget exhausted",
		ArtifactsJSON:  "[]",
	})
	if err == nil {
		t.Fatal("ResolveStalledRun() error = nil, want lease release failure")
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "running" {
		t.Fatalf("task status = %q, want running after rollback", gotTask.Status)
	}
	if gotTask.CurrentRunID == nil || *gotTask.CurrentRunID != run.ID {
		t.Fatalf("task current run = %v, want %d after rollback", gotTask.CurrentRunID, run.ID)
	}

	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "running" {
		t.Fatalf("run status = %q, want running after rollback", gotRun.Status)
	}

	gotLease, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if gotLease.State != "active" {
		t.Fatalf("lease state = %q, want active after rollback", gotLease.State)
	}
}
