package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestWorktreeLeaseCreateHeartbeatReleaseAndConflict(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "worktree-leases.db")
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

	if lease.Mode != "mutable" {
		t.Fatalf("CreateWorktreeLease().Mode = %q, want %q", lease.Mode, "mutable")
	}
	if lease.ReleasedAt != nil {
		t.Fatalf("CreateWorktreeLease().ReleasedAt = %v, want nil", lease.ReleasedAt)
	}

	heartbeat, err := store.HeartbeatWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("HeartbeatWorktreeLease() error = %v", err)
	}
	if !heartbeat.HeartbeatAt.After(lease.HeartbeatAt) && !heartbeat.HeartbeatAt.Equal(lease.HeartbeatAt) {
		t.Fatalf("HeartbeatWorktreeLease().HeartbeatAt = %v, want updated value after %v", heartbeat.HeartbeatAt, lease.HeartbeatAt)
	}

	if _, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: "/tmp/odin/cfipros/task-1/run-1/try-2",
		RepoRoot:     project.GitRoot,
		State:        "active",
	}); err == nil {
		t.Fatalf("CreateWorktreeLease(conflicting task) error = nil, want conflict")
	}

	if _, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID + 1,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-2/run-1/try-1",
		WorktreePath: lease.WorktreePath,
		RepoRoot:     project.GitRoot,
		State:        "active",
	}); err == nil {
		t.Fatalf("CreateWorktreeLease(conflicting path) error = nil, want conflict")
	}

	released, err := store.ReleaseWorktreeLease(ctx, ReleaseWorktreeLeaseParams{
		LeaseID: lease.ID,
		State:   "released",
	})
	if err != nil {
		t.Fatalf("ReleaseWorktreeLease() error = %v", err)
	}
	if released.ReleasedAt == nil {
		t.Fatalf("ReleaseWorktreeLease().ReleasedAt = nil, want value")
	}
}

func TestCleanupEligibleWorktreeLeasesIncludesReleasedAndStale(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "worktree-lease-cleanup.db")
	defer store.Close()

	project, task, run := seedContextPacketTask(t, ctx, store)

	released, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
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
		t.Fatalf("CreateWorktreeLease(released) error = %v", err)
	}

	released, err = store.ReleaseWorktreeLease(ctx, ReleaseWorktreeLeaseParams{
		LeaseID: released.ID,
		State:   "released",
	})
	if err != nil {
		t.Fatalf("ReleaseWorktreeLease() error = %v", err)
	}

	staleTask, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "wake-packet-stale",
		Title:       "Prepare stale lease",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(stale) error = %v", err)
	}
	staleRun, err := store.StartRun(ctx, StartRunParams{
		TaskID:   staleTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(stale) error = %v", err)
	}

	stale, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       staleTask.ID,
		RunID:        staleRun.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-2/run-1/try-1",
		WorktreePath: "/tmp/odin/cfipros/task-2/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(stale) error = %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `
		UPDATE worktree_leases
		SET heartbeat_at = ?, updated_at = ?
		WHERE id = ?
	`, formatTime(time.Now().UTC().Add(-2*time.Hour)), formatTime(time.Now().UTC().Add(-2*time.Hour)), stale.ID); err != nil {
		t.Fatalf("force stale heartbeat error = %v", err)
	}

	activeTask, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "wake-packet-active",
		Title:       "Prepare active lease",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(active) error = %v", err)
	}
	activeRun, err := store.StartRun(ctx, StartRunParams{
		TaskID:   activeTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(active) error = %v", err)
	}

	active, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       activeTask.ID,
		RunID:        activeRun.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-3/run-1/try-1",
		WorktreePath: "/tmp/odin/cfipros/task-3/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(active) error = %v", err)
	}

	eligible, err := store.ListCleanupEligibleWorktreeLeases(ctx, time.Now().UTC().Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("ListCleanupEligibleWorktreeLeases() error = %v", err)
	}

	if len(eligible) != 2 {
		t.Fatalf("ListCleanupEligibleWorktreeLeases() len = %d, want 2", len(eligible))
	}

	found := map[int64]bool{}
	for _, lease := range eligible {
		found[lease.ID] = true
	}
	if !found[released.ID] {
		t.Fatalf("released lease %d missing from cleanup list", released.ID)
	}
	if !found[stale.ID] {
		t.Fatalf("stale lease %d missing from cleanup list", stale.ID)
	}
	if found[active.ID] {
		t.Fatalf("active lease %d unexpectedly marked cleanup eligible", active.ID)
	}
}

func TestListActiveWorktreeLeasesReturnsOnlyActive(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "worktree-lease-active-list.db")
	defer store.Close()

	project, task, run := seedContextPacketTask(t, ctx, store)

	active, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
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
		t.Fatalf("CreateWorktreeLease(active) error = %v", err)
	}

	releasedTask, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "wake-packet-released",
		Title:       "Prepare released lease",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(released) error = %v", err)
	}
	releasedRun, err := store.StartRun(ctx, StartRunParams{
		TaskID:   releasedTask.ID,
		Executor: "codex",
		Attempt:  2,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(released) error = %v", err)
	}

	released, err := store.CreateWorktreeLease(ctx, CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       releasedTask.ID,
		RunID:        releasedRun.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-2/run-1/try-1",
		WorktreePath: "/tmp/odin/cfipros/task-2/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(released) error = %v", err)
	}
	if _, err := store.ReleaseWorktreeLease(ctx, ReleaseWorktreeLeaseParams{
		LeaseID: released.ID,
		State:   "released",
	}); err != nil {
		t.Fatalf("ReleaseWorktreeLease() error = %v", err)
	}

	activeLeases, err := store.ListActiveWorktreeLeases(ctx)
	if err != nil {
		t.Fatalf("ListActiveWorktreeLeases() error = %v", err)
	}
	if len(activeLeases) != 1 {
		t.Fatalf("ListActiveWorktreeLeases() len = %d, want 1", len(activeLeases))
	}
	if activeLeases[0].ID != active.ID {
		t.Fatalf("ListActiveWorktreeLeases()[0].ID = %d, want %d", activeLeases[0].ID, active.ID)
	}
}
