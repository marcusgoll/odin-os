package leases

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/worktrees"
)

func TestMaintenanceHeartbeatActiveLeases(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
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

	maint := Maintenance{
		Store: store,
		Now: func() time.Time {
			return lease.HeartbeatAt.Add(30 * time.Second)
		},
	}
	store.Now = maint.Now

	result, err := maint.HeartbeatActive(ctx)
	if err != nil {
		t.Fatalf("HeartbeatActive() error = %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("HeartbeatActive().Updated = %d, want 1", result.Updated)
	}

	updated, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if !updated.HeartbeatAt.After(lease.HeartbeatAt) {
		t.Fatalf("HeartbeatAt = %v, want later than %v", updated.HeartbeatAt, lease.HeartbeatAt)
	}
}

func TestMaintenanceCleanupExpiredRemovesReleasedAndStaleLeases(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	released, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: filepath.ToSlash(filepath.Join(t.TempDir(), "released")),
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(released) error = %v", err)
	}
	if _, err := store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: released.ID,
		State:   "released",
	}); err != nil {
		t.Fatalf("ReleaseWorktreeLease() error = %v", err)
	}

	staleTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "stale-cleanup",
		Title:       "Cleanup stale lease",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(stale) error = %v", err)
	}
	staleRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   staleTask.ID,
		Executor: "codex",
		Attempt:  2,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(stale) error = %v", err)
	}
	stale, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       staleTask.ID,
		RunID:        staleRun.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-2/run-2/try-1",
		WorktreePath: filepath.ToSlash(filepath.Join(t.TempDir(), "stale")),
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(stale) error = %v", err)
	}
	staleAt := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE worktree_leases
		SET heartbeat_at = ?, updated_at = ?
		WHERE id = ?
	`, staleAt.Format(time.RFC3339Nano), staleAt.Format(time.RFC3339Nano), stale.ID); err != nil {
		t.Fatalf("force stale heartbeat error = %v", err)
	}

	activeTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "active-cleanup",
		Title:       "Keep active lease",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(active) error = %v", err)
	}
	activeRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   activeTask.ID,
		Executor: "codex",
		Attempt:  3,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(active) error = %v", err)
	}
	active, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       activeTask.ID,
		RunID:        activeRun.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-3/run-3/try-1",
		WorktreePath: filepath.ToSlash(filepath.Join(t.TempDir(), "active")),
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(active) error = %v", err)
	}

	git := &fakeCleanupGit{}
	maint := Maintenance{
		Store: store,
		Cleanup: worktrees.Manager{
			Store: store,
			Git:   git,
		},
		Now: func() time.Time {
			return staleAt.Add(2 * time.Hour)
		},
	}

	result, err := maint.CleanupExpired(ctx, 30*time.Minute)
	if err != nil {
		t.Fatalf("CleanupExpired() error = %v", err)
	}
	if len(result.Removed) != 2 {
		t.Fatalf("CleanupExpired().Removed len = %d, want 2", len(result.Removed))
	}
	if git.removeCalls != 2 {
		t.Fatalf("cleanup git remove calls = %d, want 2", git.removeCalls)
	}

	activeLease, err := store.GetWorktreeLease(ctx, active.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease(active) error = %v", err)
	}
	if activeLease.State != "active" {
		t.Fatalf("GetWorktreeLease(active).State = %q, want active", activeLease.State)
	}
}

type fakeCleanupGit struct {
	removeCalls int
}

func (git *fakeCleanupGit) RemoveWorktree(context.Context, string, string) error {
	git.removeCalls++
	return nil
}
