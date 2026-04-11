package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRepoRootFallsBackToExecutableLocation(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "config"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "config", "odin.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(odin.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "bin", "odin"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(binary) error = %v", err)
	}

	installDir := filepath.Join(t.TempDir(), ".local", "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(installDir) error = %v", err)
	}
	installedPath := filepath.Join(installDir, "odin")
	if err := os.Symlink(filepath.Join(repoRoot, "bin", "odin"), installedPath); err != nil {
		t.Fatalf("Symlink(installedPath) error = %v", err)
	}

	cwd := t.TempDir()
	got, err := resolveRepoRoot(cwd, installedPath)
	if err != nil {
		t.Fatalf("resolveRepoRoot() error = %v", err)
	}
	if got != repoRoot {
		t.Fatalf("resolveRepoRoot() = %q, want %q", got, repoRoot)
	}
}

func TestResolveRepoRootPrefersCurrentWorkingDirectoryWhenAlreadyInRepo(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "config"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "config", "odin.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(odin.yaml) error = %v", err)
	}

	executable := filepath.Join(t.TempDir(), "odin")
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(executable) error = %v", err)
	}

	got, err := resolveRepoRoot(cwd, executable)
	if err != nil {
		t.Fatalf("resolveRepoRoot() error = %v", err)
	}
	if got != cwd {
		t.Fatalf("resolveRepoRoot() = %q, want %q", got, cwd)
	}
}

func TestRunReturnsCleanExitOnCanceledLifecycle(t *testing.T) {
	t.Parallel()

	originalRunLifecycle := runLifecycle
	runLifecycle = func(context.Context, string, []string, io.Reader, io.Writer) error {
		return context.Canceled
	}
	defer func() {
		runLifecycle = originalRunLifecycle
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), t.TempDir(), nil, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty for clean cancellation", stderr.String())
	}
}
