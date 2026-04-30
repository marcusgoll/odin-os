package leases

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestManagerPrepareMutableAllocatesBranchAndWorktree(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	git := &fakeGit{}
	worktreeRoot := t.TempDir()
	manager := Manager{
		Store:        store,
		Git:          git,
		WorktreeRoot: worktreeRoot,
	}

	assignment, err := manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err != nil {
		t.Fatalf("Prepare(mutable) error = %v", err)
	}

	if assignment.Mode != "mutable" {
		t.Fatalf("Prepare(mutable).Mode = %q, want %q", assignment.Mode, "mutable")
	}
	wantBranch := fmt.Sprintf("odin/%s/task-%d/run-%d/try-1", project.Key, task.ID, run.ID)
	if assignment.BranchName != wantBranch {
		t.Fatalf("Prepare(mutable).BranchName = %q", assignment.BranchName)
	}
	wantPath := filepath.ToSlash(filepath.Join(worktreeRoot, project.Key, fmt.Sprintf("task-%d", task.ID), fmt.Sprintf("run-%d", run.ID), "try-1"))
	if assignment.WorktreePath != wantPath {
		t.Fatalf("Prepare(mutable).WorktreePath = %q", assignment.WorktreePath)
	}
	if git.createBranchCalls != 1 || git.addWorktreeCalls != 1 {
		t.Fatalf("git calls = create:%d add:%d, want 1/1", git.createBranchCalls, git.addWorktreeCalls)
	}
}

func TestManagerIsCanonicalWorkspaceManager(t *testing.T) {
	t.Parallel()

	var _ WorkspaceManager = Manager{}
}

func TestManagerPrepareReadOnlySkipsMutableAllocation(t *testing.T) {
	t.Parallel()

	manager := Manager{
		Git:          &fakeGit{},
		WorktreeRoot: t.TempDir(),
	}

	assignment, err := manager.Prepare(context.Background(), Request{
		Mutating: false,
		RepoRoot: "/home/orchestrator/projects/cfipros",
	})
	if err != nil {
		t.Fatalf("Prepare(read-only) error = %v", err)
	}
	if assignment.Mode != "read_only" {
		t.Fatalf("Prepare(read-only).Mode = %q, want %q", assignment.Mode, "read_only")
	}
	if assignment.WorktreePath != "/home/orchestrator/projects/cfipros" {
		t.Fatalf("Prepare(read-only).WorktreePath = %q", assignment.WorktreePath)
	}
}

func TestManagerPrepareMutableReusesActiveLeaseForSameTaskRun(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	git := &fakeGit{}
	manager := Manager{
		Store:        store,
		Git:          git,
		WorktreeRoot: t.TempDir(),
	}

	first, err := manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err != nil {
		t.Fatalf("Prepare(first) error = %v", err)
	}

	second, err := manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err != nil {
		t.Fatalf("Prepare(second) error = %v", err)
	}

	if !second.Reused {
		t.Fatalf("Prepare(second).Reused = false, want true")
	}
	if second.LeaseID == nil || first.LeaseID == nil || *second.LeaseID != *first.LeaseID {
		t.Fatalf("Prepare(second).LeaseID = %v, want %v", second.LeaseID, first.LeaseID)
	}
	if git.addWorktreeCalls != 1 {
		t.Fatalf("git add worktree calls = %d, want 1", git.addWorktreeCalls)
	}
}

func TestManagerPrepareMutablePropagatesLeaseConflict(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	if _, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   fmt.Sprintf("odin/%s/task-%d/run-%d/try-1", project.Key, task.ID, run.ID),
		WorktreePath: filepath.ToSlash(fmt.Sprintf("/var/tmp/odin-worktrees/%s/task-%d/run-%d/try-1", project.Key, task.ID, run.ID)),
		RepoRoot:     project.GitRoot,
		State:        "active",
	}); err != nil {
		t.Fatalf("CreateWorktreeLease(seed conflict) error = %v", err)
	}

	conflictingRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  2,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(conflicting) error = %v", err)
	}

	manager := Manager{
		Store:        store,
		Git:          &fakeGit{},
		WorktreeRoot: t.TempDir(),
	}

	_, err = manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         conflictingRun.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err == nil {
		t.Fatalf("Prepare(conflict) error = nil, want conflict")
	}
	if !errors.Is(err, sqlite.ErrWorktreeLeaseConflict) {
		t.Fatalf("Prepare(conflict) error = %v, want ErrWorktreeLeaseConflict", err)
	}
}

func TestManagerPrepareMutableSkipsExistingFilesystemPathByAdvancingTry(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	worktreeRoot := t.TempDir()
	stalePath := filepath.Join(worktreeRoot, project.Key, fmt.Sprintf("task-%d", task.ID), fmt.Sprintf("run-%d", run.ID), "try-1")
	if err := os.MkdirAll(stalePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(stalePath) error = %v", err)
	}

	git := &fakeGit{}
	manager := Manager{
		Store:        store,
		Git:          git,
		WorktreeRoot: worktreeRoot,
	}

	assignment, err := manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err != nil {
		t.Fatalf("Prepare(existing path) error = %v", err)
	}

	wantBranch := fmt.Sprintf("odin/%s/task-%d/run-%d/try-2", project.Key, task.ID, run.ID)
	if assignment.BranchName != wantBranch {
		t.Fatalf("Prepare(existing path).BranchName = %q, want %q", assignment.BranchName, wantBranch)
	}
	wantPath := filepath.ToSlash(filepath.Join(worktreeRoot, project.Key, fmt.Sprintf("task-%d", task.ID), fmt.Sprintf("run-%d", run.ID), "try-2"))
	if assignment.WorktreePath != wantPath {
		t.Fatalf("Prepare(existing path).WorktreePath = %q, want %q", assignment.WorktreePath, wantPath)
	}
	if git.addWorktreeCalls != 1 {
		t.Fatalf("git add worktree calls = %d, want 1", git.addWorktreeCalls)
	}
}

type fakeGit struct {
	createBranchCalls int
	addWorktreeCalls  int
}

func (git *fakeGit) BranchExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (git *fakeGit) CreateBranch(context.Context, string, string, string) error {
	git.createBranchCalls++
	return nil
}

func (git *fakeGit) AddWorktree(context.Context, string, string, string) error {
	git.addWorktreeCalls++
	return nil
}

func (git *fakeGit) RemoveWorktree(context.Context, string, string) error {
	return nil
}

func openLeaseManagerStore(t *testing.T) (*sqlite.Store, sqlite.Project, sqlite.Task, sqlite.Run) {
	t.Helper()

	store := openManagerTestStore(t)
	project, task, run := createProjectTaskRun(t, context.Background(), store)
	return store, project, task, run
}

func openManagerTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func createProjectTaskRun(t *testing.T, ctx context.Context, store *sqlite.Store) (sqlite.Project, sqlite.Task, sqlite.Run) {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
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

	task, run := createTaskRun(t, ctx, store, project.ID, 42)
	return project, task, run
}

func createTaskRun(t *testing.T, ctx context.Context, store *sqlite.Store, projectID int64, idBase int64) (sqlite.Task, sqlite.Run) {
	t.Helper()

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   projectID,
		Key:         fmt.Sprintf("task-key-%d", idBase),
		Title:       "Task title",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
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
	return task, run
}
