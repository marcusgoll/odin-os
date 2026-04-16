package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestResolveStalledRunRejectsFinishedOrDetachedState(t *testing.T) {
	ctx := context.Background()

	t.Run("finished run", func(t *testing.T) {
		store := openMigratedTestStore(t, "resolve-stalled-finished.db")
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
			UPDATE runs
			SET status = 'completed', finished_at = ?, summary = 'done', terminal_reason = 'completed'
			WHERE id = ?
		`, formatTime(time.Now().UTC()), run.ID); err != nil {
			t.Fatalf("force completed run error = %v", err)
		}

		err = store.ResolveStalledRun(ctx, ResolveStalledRunParams{
			RunID:          run.ID,
			TaskID:         task.ID,
			TaskStatus:     "queued",
			Summary:        "ignored",
			TerminalReason: "ignored",
			ArtifactsJSON:  "[]",
		})
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("ResolveStalledRun() error = %v, want sql.ErrNoRows", err)
		}

		gotTask, err := store.GetTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if gotTask.Status != "running" {
			t.Fatalf("task status = %q, want running", gotTask.Status)
		}
		if gotTask.CurrentRunID == nil || *gotTask.CurrentRunID != run.ID {
			t.Fatalf("task current run = %v, want %d", gotTask.CurrentRunID, run.ID)
		}

		gotRun, err := store.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if gotRun.Status != "completed" {
			t.Fatalf("run status = %q, want completed", gotRun.Status)
		}

		gotLease, err := store.GetWorktreeLease(ctx, lease.ID)
		if err != nil {
			t.Fatalf("GetWorktreeLease() error = %v", err)
		}
		if gotLease.State != "active" {
			t.Fatalf("lease state = %q, want active", gotLease.State)
		}
	})

	t.Run("detached task", func(t *testing.T) {
		store := openMigratedTestStore(t, "resolve-stalled-detached.db")
		defer store.Close()

		project, task, run := seedContextPacketTask(t, ctx, store)
		lease, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
			ProjectID:    project.ID,
			TaskID:       task.ID,
			RunID:        run.ID,
			Mode:         "mutable",
			BranchName:   "odin/cfipros/task-2/run-1/try-1",
			WorktreePath: "/tmp/odin/cfipros/task-2/run-1/try-1",
			RepoRoot:     project.GitRoot,
			State:        "active",
		})
		if err != nil {
			t.Fatalf("CreateWorktreeLease() error = %v", err)
		}

		if _, err := store.DB().ExecContext(ctx, `
			UPDATE tasks
			SET current_run_id = NULL
			WHERE id = ?
		`, task.ID); err != nil {
			t.Fatalf("detach task error = %v", err)
		}

		err = store.ResolveStalledRun(ctx, ResolveStalledRunParams{
			RunID:          run.ID,
			TaskID:         task.ID,
			TaskStatus:     "queued",
			Summary:        "ignored",
			TerminalReason: "ignored",
			ArtifactsJSON:  "[]",
		})
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("ResolveStalledRun() error = %v, want sql.ErrNoRows", err)
		}

		gotTask, err := store.GetTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if gotTask.CurrentRunID != nil {
			t.Fatalf("task current run = %v, want nil", gotTask.CurrentRunID)
		}

		gotRun, err := store.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if gotRun.Status != "running" {
			t.Fatalf("run status = %q, want running", gotRun.Status)
		}

		gotLease, err := store.GetWorktreeLease(ctx, lease.ID)
		if err != nil {
			t.Fatalf("GetWorktreeLease() error = %v", err)
		}
		if gotLease.State != "active" {
			t.Fatalf("lease state = %q, want active", gotLease.State)
		}
	})
}
