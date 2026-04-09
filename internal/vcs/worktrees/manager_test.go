package worktrees

import (
	"context"
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

type cleanupGit struct {
	removeCalls int
}

func (git *cleanupGit) RemoveWorktree(context.Context, string, string) error {
	git.removeCalls++
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
