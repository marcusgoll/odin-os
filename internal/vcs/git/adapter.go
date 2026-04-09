package git

import (
	"context"
	"fmt"
	"os/exec"
)

type Adapter struct{}

func (Adapter) BranchExists(ctx context.Context, repoRoot string, branchName string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", branchName)
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (Adapter) CreateBranch(ctx context.Context, repoRoot string, branchName string, baseBranch string) error {
	return runGit(ctx, repoRoot, "branch", branchName, baseBranch)
}

func (Adapter) AddWorktree(ctx context.Context, repoRoot string, worktreePath string, branchName string) error {
	return runGit(ctx, repoRoot, "worktree", "add", worktreePath, branchName)
}

func (Adapter) RemoveWorktree(ctx context.Context, repoRoot string, worktreePath string) error {
	return runGit(ctx, repoRoot, "worktree", "remove", "--force", worktreePath)
}

func runGit(ctx context.Context, repoRoot string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, string(output))
	}
	return nil
}
