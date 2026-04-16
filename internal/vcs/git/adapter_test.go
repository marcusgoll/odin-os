package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	worktreemgr "odin-os/internal/vcs/worktrees"
)

func TestAdapterCreatesAndRemovesWorktree(t *testing.T) {
	ctx := context.Background()
	repoRoot := initTempRepo(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	adapter := Adapter{}

	if err := adapter.CreateBranch(ctx, repoRoot, "odin/test/task-1/run-1/try-1", "main"); err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}

	exists, err := adapter.BranchExists(ctx, repoRoot, "odin/test/task-1/run-1/try-1")
	if err != nil {
		t.Fatalf("BranchExists() error = %v", err)
	}
	if !exists {
		t.Fatalf("BranchExists() = false, want true")
	}

	if err := adapter.AddWorktree(ctx, repoRoot, worktreePath, "odin/test/task-1/run-1/try-1"); err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}

	info, err := os.Stat(worktreePath)
	if err != nil {
		t.Fatalf("Stat(worktreePath) error = %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("worktree path is not a directory")
	}

	if err := adapter.RemoveWorktree(ctx, repoRoot, worktreePath); err != nil {
		t.Fatalf("RemoveWorktree() error = %v", err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree path still exists after removal")
	}
}

func TestAdapterRemoveWorktreeReportsAlreadyRemovedAfterRegistrationCleared(t *testing.T) {
	ctx := context.Background()
	repoRoot := initTempRepo(t)
	worktreePath := filepath.Join(t.TempDir(), "wt")
	adapter := Adapter{}

	if err := adapter.CreateBranch(ctx, repoRoot, "odin/test/task-2/run-1/try-1", "main"); err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}
	if err := adapter.AddWorktree(ctx, repoRoot, worktreePath, "odin/test/task-2/run-1/try-1"); err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}
	if err := os.RemoveAll(worktreePath); err != nil {
		t.Fatalf("RemoveAll(worktreePath) error = %v", err)
	}

	if err := adapter.RemoveWorktree(ctx, repoRoot, worktreePath); err != nil {
		t.Fatalf("RemoveWorktree() after manual deletion error = %v", err)
	}

	err := adapter.RemoveWorktree(ctx, repoRoot, worktreePath)
	if !errors.Is(err, worktreemgr.ErrWorktreeAlreadyRemoved) {
		t.Fatalf("RemoveWorktree() second call error = %v, want ErrWorktreeAlreadyRemoved", err)
	}
}

func TestAdapterRemoveWorktreeTimesOutWithoutCallerDeadline(t *testing.T) {
	gitDir := t.TempDir()
	scriptPath := filepath.Join(gitDir, "git")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nsleep 5\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(fake git) error = %v", err)
	}
	t.Setenv("PATH", gitDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	originalTimeout := worktreeRemoveTimeout
	worktreeRemoveTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		worktreeRemoveTimeout = originalTimeout
	})

	err := Adapter{}.RemoveWorktree(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "wt"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RemoveWorktree() error = %v, want context deadline exceeded", err)
	}
	if !strings.Contains(err.Error(), "worktree remove") {
		t.Fatalf("RemoveWorktree() error = %v, want git command context", err)
	}
}

func initTempRepo(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	repoRoot := t.TempDir()
	if err := runGit(ctx, repoRoot, "init", "-b", "main"); err != nil {
		t.Fatalf("git init error = %v", err)
	}
	if err := runGit(ctx, repoRoot, "config", "user.name", "Odin Test"); err != nil {
		t.Fatalf("git config user.name error = %v", err)
	}
	if err := runGit(ctx, repoRoot, "config", "user.email", "odin@example.com"); err != nil {
		t.Fatalf("git config user.email error = %v", err)
	}

	readmePath := filepath.Join(repoRoot, "README.md")
	if err := os.WriteFile(readmePath, []byte("# temp repo\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(README.md) error = %v", err)
	}

	if err := runGit(ctx, repoRoot, "add", "README.md"); err != nil {
		t.Fatalf("git add error = %v", err)
	}
	if err := runGit(ctx, repoRoot, "commit", "-m", "initial commit"); err != nil {
		t.Fatalf("git commit error = %v", err)
	}
	return repoRoot
}
