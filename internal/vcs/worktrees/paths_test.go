package worktrees

import "testing"

func TestDefaultRootUsesImmediateGlobalPath(t *testing.T) {
	t.Parallel()

	root := DefaultRoot()
	want := "~/.config/superpowers/worktrees/odin-os"
	if root != want {
		t.Fatalf("DefaultRoot() = %q, want %q", root, want)
	}
}

func TestLongTermDefaultsDocumentLocalAndServerRoots(t *testing.T) {
	t.Parallel()

	defaults := LongTermDefaults()
	if defaults.LocalDevelopmentRoot != "~/.local/share/superpowers/worktrees/odin-os" {
		t.Fatalf("LocalDevelopmentRoot = %q", defaults.LocalDevelopmentRoot)
	}
	if defaults.ServerRuntimeRoot != "/var/odin/worktrees/odin-os" {
		t.Fatalf("ServerRuntimeRoot = %q", defaults.ServerRuntimeRoot)
	}
}

func TestResolvePathBuildsTaskOwnedWorktreeOutsideRepoRoot(t *testing.T) {
	t.Parallel()

	path := ResolvePath(PathParams{
		Root:       "/var/tmp/odin-worktrees",
		ProjectKey: "cfipros",
		TaskID:     42,
		RunID:      9,
		Try:        1,
	})

	want := "/var/tmp/odin-worktrees/cfipros/task-42/run-9/try-1"
	if path != want {
		t.Fatalf("ResolvePath() = %q, want %q", path, want)
	}
	if path == "/home/orchestrator/projects/cfipros" {
		t.Fatalf("ResolvePath() reused repo root")
	}
}
