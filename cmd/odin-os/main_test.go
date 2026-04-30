package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRepoRootFromOdinOSExecutable(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "config"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "config", "odin.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(odin.yaml) error = %v", err)
	}

	binDir := filepath.Join(repoRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	executable := filepath.Join(binDir, "odin-os")
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(executable) error = %v", err)
	}

	got, err := resolveRepoRoot(t.TempDir(), executable)
	if err != nil {
		t.Fatalf("resolveRepoRoot() error = %v", err)
	}
	if got != repoRoot {
		t.Fatalf("resolveRepoRoot() = %q, want %q", got, repoRoot)
	}
}
