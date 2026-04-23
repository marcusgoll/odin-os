package projects

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManagedProjectInputManifestUsesDefaultPolicy(t *testing.T) {
	t.Parallel()

	manifest := ManagedProjectInput{
		Key:           "family-ops",
		GitRoot:       "/tmp/family-ops",
		DefaultBranch: "main",
	}.Manifest()

	if manifest.ProjectClass != ProjectClassLocalGit {
		t.Fatalf("ProjectClass = %q, want %q", manifest.ProjectClass, ProjectClassLocalGit)
	}
	if manifest.Name != "family-ops" {
		t.Fatalf("Name = %q, want family-ops", manifest.Name)
	}
	if manifest.Policy.MergePolicy.Mode != "squash" {
		t.Fatalf("MergePolicy.Mode = %q, want squash", manifest.Policy.MergePolicy.Mode)
	}
}

func TestManagedProjectInputManifestPromotesGitHubBackedClass(t *testing.T) {
	t.Parallel()

	manifest := ManagedProjectInput{
		Key:           "family-ops",
		GitRoot:       "/tmp/family-ops",
		DefaultBranch: "main",
		GitHubRepo:    "acme/family-ops",
	}.Manifest()

	if manifest.ProjectClass != ProjectClassGitHubBacked {
		t.Fatalf("ProjectClass = %q, want %q", manifest.ProjectClass, ProjectClassGitHubBacked)
	}
}

func TestInferGitRootAndDefaultBranch(t *testing.T) {
	ctx := context.Background()
	repoRoot := filepath.Join(t.TempDir(), "family-ops")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(repoRoot) error = %v", err)
	}

	runGitCommand(t, repoRoot, "init", "-b", "main")
	runGitCommand(t, repoRoot, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	runGitCommand(t, repoRoot, "remote", "add", "origin", "https://example.com/acme/family-ops.git")
	runGitCommand(t, repoRoot, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGitCommand(t, repoRoot, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	subdir := filepath.Join(repoRoot, "src")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(subdir) error = %v", err)
	}

	inferredRoot, err := InferGitRoot(ctx, subdir)
	if err != nil {
		t.Fatalf("InferGitRoot() error = %v", err)
	}
	if inferredRoot != repoRoot {
		t.Fatalf("InferGitRoot() = %q, want %q", inferredRoot, repoRoot)
	}

	branch, err := InferDefaultBranch(ctx, repoRoot)
	if err != nil {
		t.Fatalf("InferDefaultBranch() error = %v", err)
	}
	if branch != "main" {
		t.Fatalf("InferDefaultBranch() = %q, want main", branch)
	}
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	commandArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", commandArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error = %v\n%s", args, err, output)
	}
}
