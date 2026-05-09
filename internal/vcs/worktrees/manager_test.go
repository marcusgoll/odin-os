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
	manager := Manager{Store: store, Git: git, WorktreeRoot: "/var/tmp/odin-worktrees"}

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
	if git.dirtyCalls != 1 {
		t.Fatalf("git dirty calls = %d, want 1", git.dirtyCalls)
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
	manager := Manager{Store: store, Git: git, WorktreeRoot: "/var/tmp/odin-worktrees"}

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
	if git.dirtyCalls != 0 {
		t.Fatalf("git dirty calls = %d, want 0", git.dirtyCalls)
	}

	updated, err := store.GetWorktreeLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if updated.CleanedUpAt != nil {
		t.Fatalf("GetWorktreeLease().CleanedUpAt = %v, want nil", updated.CleanedUpAt)
	}
}

func TestManagerCleanupLeasesRemovesSelectedReleasedLeases(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openCleanupStore(t)
	defer store.Close()

	released, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
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
		t.Fatalf("CreateWorktreeLease(released) error = %v", err)
	}
	released, err = store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: released.ID,
		State:   "released",
	})
	if err != nil {
		t.Fatalf("ReleaseWorktreeLease(released) error = %v", err)
	}

	other, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-2",
		WorktreePath: "/var/tmp/odin-worktrees/cfipros/task-1/run-1/try-2",
		RepoRoot:     project.GitRoot,
		State:        "active",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease(other) error = %v", err)
	}
	other, err = store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: other.ID,
		State:   "released",
	})
	if err != nil {
		t.Fatalf("ReleaseWorktreeLease(other) error = %v", err)
	}

	git := &cleanupGit{}
	manager := Manager{Store: store, Git: git, WorktreeRoot: "/var/tmp/odin-worktrees"}

	result, err := manager.CleanupLeases(ctx, []sqlite.WorktreeLease{released})
	if err != nil {
		t.Fatalf("CleanupLeases() error = %v", err)
	}
	if len(result.Removed) != 1 {
		t.Fatalf("CleanupLeases().Removed len = %d, want 1", len(result.Removed))
	}
	if git.dirtyCalls != 1 {
		t.Fatalf("git dirty calls = %d, want 1", git.dirtyCalls)
	}
	if result.Removed[0].ID != released.ID {
		t.Fatalf("CleanupLeases().Removed[0].ID = %d, want %d", result.Removed[0].ID, released.ID)
	}

	cleaned, err := store.GetWorktreeLease(ctx, released.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease(released) error = %v", err)
	}
	if cleaned.CleanedUpAt == nil || cleaned.State != "cleaned" {
		t.Fatalf("cleaned lease = %+v, want cleaned", cleaned)
	}

	untouched, err := store.GetWorktreeLease(ctx, other.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease(other) error = %v", err)
	}
	if untouched.CleanedUpAt != nil || untouched.State != "released" {
		t.Fatalf("untouched lease = %+v, want released and not cleaned", untouched)
	}
}

func TestManagerCleanupLeasesRejectsDirtyWorktreeByDefault(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openCleanupStore(t)
	defer store.Close()

	released, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: "/var/tmp/odin-worktrees/cfipros/task-1/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "released",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	git := &cleanupGit{dirty: true}
	manager := Manager{Store: store, Git: git, WorktreeRoot: "/var/tmp/odin-worktrees"}

	result, err := manager.CleanupLeases(ctx, []sqlite.WorktreeLease{released})
	if !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("CleanupLeases(dirty) error = %v, want ErrDirtyWorktree", err)
	}
	if len(result.Removed) != 0 {
		t.Fatalf("CleanupLeases(dirty).Removed len = %d, want 0", len(result.Removed))
	}
	if git.dirtyCalls != 1 {
		t.Fatalf("dirty calls = %d, want 1", git.dirtyCalls)
	}
	if git.removeCalls != 0 {
		t.Fatalf("remove calls = %d, want 0", git.removeCalls)
	}

	unchanged, err := store.GetWorktreeLease(ctx, released.ID)
	if err != nil {
		t.Fatalf("GetWorktreeLease() error = %v", err)
	}
	if unchanged.CleanedUpAt != nil || unchanged.State != "released" {
		t.Fatalf("dirty lease = %+v, want released and not cleaned", unchanged)
	}
}

func TestManagerCleanupLeasesForceDirtyRequiresApproval(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openCleanupStore(t)
	defer store.Close()

	released, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: "/var/tmp/odin-worktrees/cfipros/task-1/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "released",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	git := &cleanupGit{dirty: true}
	manager := Manager{Store: store, Git: git, WorktreeRoot: "/var/tmp/odin-worktrees"}

	result, err := manager.CleanupLeasesWithOptions(ctx, []sqlite.WorktreeLease{released}, CleanupOptions{
		ForceDirty: true,
	})
	if err == nil {
		t.Fatalf("CleanupLeasesWithOptions(force without approval) error = nil, want approval error")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("CleanupLeasesWithOptions(force without approval).Removed len = %d, want 0", len(result.Removed))
	}
	if git.dirtyCalls != 0 || git.removeCalls != 0 {
		t.Fatalf("git calls dirty=%d remove=%d, want 0/0 before approval", git.dirtyCalls, git.removeCalls)
	}
}

func TestManagerCleanupLeasesForceDirtyWithApprovalRemovesWorktree(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openCleanupStore(t)
	defer store.Close()

	released, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: "/var/tmp/odin-worktrees/cfipros/task-1/run-1/try-1",
		RepoRoot:     project.GitRoot,
		State:        "released",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	git := &cleanupGit{dirty: true}
	manager := Manager{Store: store, Git: git, WorktreeRoot: "/var/tmp/odin-worktrees"}

	result, err := manager.CleanupLeasesWithOptions(ctx, []sqlite.WorktreeLease{released}, CleanupOptions{
		ForceDirty:     true,
		ApprovedBy:     "operator",
		ApprovalReason: "explicit cleanup test",
	})
	if err != nil {
		t.Fatalf("CleanupLeasesWithOptions(force approved) error = %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0].ID != released.ID {
		t.Fatalf("CleanupLeasesWithOptions(force approved).Removed = %+v, want lease %d", result.Removed, released.ID)
	}
	if git.dirtyCalls != 1 || git.removeCalls != 1 {
		t.Fatalf("git calls dirty=%d remove=%d, want 1/1", git.dirtyCalls, git.removeCalls)
	}
}

func TestManagerCleanupLeasesRejectsPathOutsideWorkspaceRoot(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openCleanupStore(t)
	defer store.Close()

	released, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   "odin/cfipros/task-1/run-1/try-1",
		WorktreePath: "/tmp/outside",
		RepoRoot:     project.GitRoot,
		State:        "released",
	})
	if err != nil {
		t.Fatalf("CreateWorktreeLease() error = %v", err)
	}

	git := &cleanupGit{}
	manager := Manager{Store: store, Git: git, WorktreeRoot: "/var/tmp/odin-worktrees"}

	result, err := manager.CleanupLeases(ctx, []sqlite.WorktreeLease{released})
	if err == nil {
		t.Fatalf("CleanupLeases() error = nil, want path boundary error")
	}
	if len(result.Removed) != 0 {
		t.Fatalf("CleanupLeases().Removed len = %d, want 0", len(result.Removed))
	}
	if git.removeCalls != 0 {
		t.Fatalf("git remove calls = %d, want 0", git.removeCalls)
	}
	if git.dirtyCalls != 0 {
		t.Fatalf("git dirty calls = %d, want 0", git.dirtyCalls)
	}
}

type cleanupGit struct {
	removeCalls int
	dirtyCalls  int
	dirty       bool
}

func (git *cleanupGit) RemoveWorktree(context.Context, string, string) error {
	git.removeCalls++
	return nil
}

func (git *cleanupGit) WorktreeDirty(context.Context, string) (bool, error) {
	git.dirtyCalls++
	return git.dirty, nil
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
