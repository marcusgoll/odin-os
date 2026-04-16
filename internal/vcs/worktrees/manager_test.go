package worktrees

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/store/sqlite"
)

func TestManagerCleanupRemovesReleasedLeaseDeterministically(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openCleanupStore(t)
	defer store.Close()

	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: "/var/tmp/odin-worktrees/cfipros/task-1/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	if _, err := store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: lease.ID,
		State:   "released",
	}); err != nil {
		t.Fatalf("ReleaseWorktreeLease() error = %v", err)
	}

	git := &cleanupGit{}
	manager := Manager{Store: store, Git: git}

	result, err := manager.Cleanup(ctx, time.Now().UTC().Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(result.Removed) != 1 {
		t.Fatalf("Cleanup().Removed len = %d, want 1", len(result.Removed))
	}
	if git.removeCalls != 1 {
		t.Fatalf("git remove calls = %d, want 1", git.removeCalls)
	}

	updated, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if updated.CleanedUpAt == nil {
		t.Fatalf("GetWorktreeLease().CleanedUpAt = nil, want value")
	}
	if updated.State != "cleaned" {
		t.Fatalf("GetWorktreeLease().State = %q, want %q", updated.State, "cleaned")
	}
}

func TestManagerCleanupPreservesActiveLease(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openCleanupStore(t)
	defer store.Close()

	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: "/var/tmp/odin-worktrees/cfipros/task-1/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	git := &cleanupGit{}
	manager := Manager{Store: store, Git: git}

	result, err := manager.Cleanup(ctx, time.Now().UTC().Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(result.Removed) != 0 {
		t.Fatalf("Cleanup().Removed len = %d, want 0", len(result.Removed))
	}
	if git.removeCalls != 0 {
		t.Fatalf("git remove calls = %d, want 0", git.removeCalls)
	}

	updated, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if updated.CleanedUpAt != nil {
		t.Fatalf("GetWorktreeLease().CleanedUpAt = %v, want nil", updated.CleanedUpAt)
	}
}

func TestManagerCleanupRemovesStaleActiveLeaseDeterministically(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openCleanupStore(t)
	defer store.Close()

	lease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: "/var/tmp/odin-worktrees/cfipros/task-1/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	forceLeaseHeartbeatAt(t, ctx, store, lease.ID, time.Now().UTC().Add(-2*time.Hour))

	git := &cleanupGit{}
	manager := Manager{Store: store, Git: git}

	result, err := manager.Cleanup(ctx, time.Now().UTC().Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if len(result.Removed) != 1 {
		t.Fatalf("Cleanup().Removed len = %d, want 1", len(result.Removed))
	}
	if git.removeCalls != 1 {
		t.Fatalf("git remove calls = %d, want 1", git.removeCalls)
	}

	updated, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if updated.State != "cleaned" {
		t.Fatalf("GetWorktreeLease().State = %q, want %q", updated.State, "cleaned")
	}
	if updated.CleanedUpAt == nil {
		t.Fatalf("GetWorktreeLease().CleanedUpAt = nil, want value")
	}
}

func TestManagerCleanupContinuesPastLeaseRemovalFailure(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openCleanupStore(t)
	defer store.Close()

	failingLease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: "/var/tmp/odin-worktrees/cfipros/task-1/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(failing) error = %v", err)
	}
	if _, err := store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: failingLease.ID,
		State:   "released",
	}); err != nil {
		t.Fatalf("ReleaseWorktreeLease(failing) error = %v", err)
	}

	nextTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "phase-09-cleanup-next",
		Title:       "Cleanup second worktree",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(next) error = %v", err)
	}
	nextRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   nextTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(next) error = %v", err)
	}
	succeedingLease, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       nextTask.ID,
		RunID:        nextRun.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-2/run-1/try-1",
		WorktreePath: "/var/tmp/odin-worktrees/cfipros/task-2/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(succeeding) error = %v", err)
	}
	if _, err := store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: succeedingLease.ID,
		State:   "released",
	}); err != nil {
		t.Fatalf("ReleaseWorktreeLease(succeeding) error = %v", err)
	}

	removeErr := errors.New("remove failed")
	git := &cleanupGit{
		errsByPath: map[string]error{
			failingLease.WorktreePath: removeErr,
		},
	}
	manager := Manager{Store: store, Git: git}

	result, err := manager.Cleanup(ctx, time.Now().UTC().Add(-30*time.Minute))
	if !errors.Is(err, removeErr) {
		t.Fatalf("Cleanup() error = %v, want remove failure", err)
	}
	if len(result.Removed) != 1 {
		t.Fatalf("Cleanup().Removed len = %d, want 1", len(result.Removed))
	}
	if result.Removed[0].ID != succeedingLease.ID {
		t.Fatalf("Cleanup().Removed[0].ID = %d, want %d", result.Removed[0].ID, succeedingLease.ID)
	}

	failingAfter, err := store.GetWorktreeLease(ctx, failingLease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease(failing) error = %v", err)
	}
	if failingAfter.State != "released" {
		t.Fatalf("failing lease state = %q, want released", failingAfter.State)
	}
	if failingAfter.CleanedUpAt != nil {
		t.Fatalf("failing lease cleaned unexpectedly")
	}

	succeedingAfter, err := store.GetWorktreeLease(ctx, succeedingLease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease(succeeding) error = %v", err)
	}
	if succeedingAfter.State != "cleaned" {
		t.Fatalf("succeeding lease state = %q, want cleaned", succeedingAfter.State)
	}
	if succeedingAfter.CleanedUpAt == nil {
		t.Fatalf("succeeding lease cleaned_up_at = nil, want value")
	}
}

type cleanupGit struct {
	removeCalls int
	errsByPath  map[string]error
}

func (git *cleanupGit) RemoveWorktree(_ context.Context, _ string, worktreePath string) error {
	git.removeCalls++
	if git.errsByPath != nil {
		if err, ok := git.errsByPath[worktreePath]; ok {
			return err
		}
	}
	return nil
}

func openCleanupStore(t *testing.T) (*sqlite.Store, sqlite.Project, sqlite.Task, sqlite.Run) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "odin.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(context.Background(), sqlite.CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFI Pros",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/projects/cfipros",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(context.Background(), sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "phase-09-cleanup",
		Title:       "Cleanup worktree",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(context.Background(), sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	return store, project, task, run
}

func forceLeaseHeartbeatAt(t *testing.T, ctx context.Context, store *sqlite.Store, leaseID int64, when time.Time) {
	t.Helper()

	formatted := when.UTC().Format(time.RFC3339Nano)
	if _, err := store.DB().ExecContext(ctx, `
		UPDATE worktree_leases
		SET heartbeat_at = ?, updated_at = ?
		WHERE id = ?
	`, formatted, formatted, leaseID); err != nil {
		t.Fatalf("force lease heartbeat error = %v", err)
	}
}
