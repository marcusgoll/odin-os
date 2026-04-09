package worktrees

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestResolvePathExpandsHomeDirectoryDefaultRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	path := ResolvePath(PathParams{
		ProjectKey: "cfipros",
		TaskID:     42,
		RunID:      9,
		Try:        1,
	})

	wantPrefix := filepath.ToSlash(filepath.Join(homeDir, ".config", "superpowers", "worktrees", "odin-os"))
	if len(path) < len(wantPrefix) || path[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("ResolvePath() = %q, want prefix %q", path, wantPrefix)
	}
	if len(path) > 0 && path[0] == '~' {
		t.Fatalf("ResolvePath() preserved literal home shortcut: %q", path)
	}
	if _, err := os.Stat(homeDir); err != nil {
		t.Fatalf("home directory stat error = %v", err)
	}
}
