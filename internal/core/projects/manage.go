package projects

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type ManagedProjectInput struct {
	Key           string
	GitRoot       string
	Name          string
	ProjectClass  ProjectClass
	DefaultBranch string
	GitHubRepo    string
}

func (input ManagedProjectInput) Manifest() Manifest {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = strings.TrimSpace(input.Key)
	}

	projectClass := input.ProjectClass
	if projectClass == "" {
		projectClass = ProjectClassLocalGit
	}
	if strings.TrimSpace(input.GitHubRepo) != "" && projectClass == ProjectClassLocalGit {
		projectClass = ProjectClassGitHubBacked
	}

	return Manifest{
		Key:           strings.TrimSpace(input.Key),
		Name:          name,
		ProjectClass:  projectClass,
		GitRoot:       strings.TrimSpace(input.GitRoot),
		DefaultBranch: strings.TrimSpace(input.DefaultBranch),
		GitHub: GitHub{
			Repo: strings.TrimSpace(input.GitHubRepo),
		},
		Policy: DefaultManagedProjectPolicy(),
	}
}

func DefaultManagedProjectPolicy() Policy {
	trueValue := true
	falseValue := false

	return Policy{
		AllowedCommands: []string{"status", "test"},
		BranchRules: BranchRules{
			ProtectedBranches:          []string{"main"},
			RequireWorktree:            &trueValue,
			RequireTaskBranch:          &trueValue,
			AllowDefaultBranchMutation: &falseValue,
		},
		ApprovalGates: ApprovalGates{
			RequireForGovernanceChanges:     &trueValue,
			RequireForDestructiveOperations: &trueValue,
			RequireForSystemProjectChanges:  &falseValue,
		},
		MergePolicy: MergePolicy{
			Mode:                       "squash",
			AllowDirectToDefaultBranch: &falseValue,
		},
		DestructiveOperations: DestructiveOperations{
			AllowReset:              &falseValue,
			AllowClean:              &falseValue,
			AllowForcePush:          &falseValue,
			RequireExplicitApproval: &trueValue,
		},
	}
}

func DeriveProjectKey(gitRoot string) string {
	if strings.TrimSpace(gitRoot) == "" {
		return ""
	}
	return strings.TrimSpace(filepath.Base(filepath.Clean(gitRoot)))
}

func InferGitRoot(ctx context.Context, cwd string) (string, error) {
	output, err := runGit(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(output), nil
}

func InferDefaultBranch(ctx context.Context, gitRoot string) (string, error) {
	output, err := runGit(ctx, gitRoot, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		branch := strings.TrimSpace(output)
		branch = strings.TrimPrefix(branch, "origin/")
		if branch != "" {
			return branch, nil
		}
	}

	return InferCurrentBranch(ctx, gitRoot)
}

func InferCurrentBranch(ctx context.Context, gitRoot string) (string, error) {
	output, err := runGit(ctx, gitRoot, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("git branch --show-current: %w", err)
	}
	branch := strings.TrimSpace(output)
	if branch == "" {
		return "", fmt.Errorf("empty branch name")
	}
	return branch, nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	commandArgs := append([]string(nil), args...)
	if strings.TrimSpace(dir) != "" {
		commandArgs = append([]string{"-C", dir}, commandArgs...)
	}

	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
