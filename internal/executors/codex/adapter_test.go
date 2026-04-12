package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/executors/contract"
)

func TestHeadlessRunTaskUsesDriverScript(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER_MODE", "fixture")

	executor := NewHeadless()
	result, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "say ready",
		Metadata: map[string]string{
			"project_key": "alpha",
		},
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if result.Metadata["driver"] != "codex_headless_script" {
		t.Fatalf("driver metadata = %q, want codex_headless_script", result.Metadata["driver"])
	}
}

func TestCodexDriverPathPrefersRuntimeWorkingTree(t *testing.T) {
	root := t.TempDir()
	driverPath := filepath.Join(root, "scripts", "drivers", "codex-headless.sh")
	if err := os.MkdirAll(filepath.Dir(driverPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(driver dir) error = %v", err)
	}
	if err := os.WriteFile(driverPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}

	t.Chdir(filepath.Join(root, "scripts"))

	got := codexDriverPath()
	if got != driverPath {
		t.Fatalf("codexDriverPath() = %q, want %q", got, driverPath)
	}
}

func TestHeadlessHealthRequiresExecutableDriver(t *testing.T) {
	root := t.TempDir()
	driverPath := filepath.Join(root, "driver.sh")
	if err := os.WriteFile(driverPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	t.Setenv("ODIN_CODEX_DRIVER", driverPath)

	report, err := NewHeadless().Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if report.Status != contract.HealthStatusDegraded {
		t.Fatalf("Health().Status = %q, want %q", report.Status, contract.HealthStatusDegraded)
	}
}

func TestHeadlessRunTaskWritesArtifactMetadata(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER_MODE", "fixture")

	worktreePath := t.TempDir()
	executor := NewHeadless()
	result, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "say ready",
		Metadata: map[string]string{
			"project_key":   "alpha",
			"worktree_path": worktreePath,
		},
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}

	artifactPath := result.Metadata["artifact_path"]
	if artifactPath == "" {
		t.Fatal("artifact_path empty, want persisted driver artifact")
	}
	if !filepath.IsAbs(artifactPath) {
		t.Fatalf("artifact_path = %q, want absolute path", artifactPath)
	}
	if result.Metadata["artifacts_json"] == "" {
		t.Fatal("artifacts_json empty, want persisted artifact pointer payload")
	}

	content, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("ReadFile(artifact_path) error = %v", err)
	}
	if !strings.Contains(string(content), "runtime-smoke") {
		t.Fatalf("artifact content = %q, want task id runtime-smoke", string(content))
	}
}

func TestHeadlessRunTaskUsesRunScopedArtifactPath(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER_MODE", "fixture")

	worktreePath := t.TempDir()
	executor := NewHeadless()
	first, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "say ready",
		Metadata: map[string]string{
			"project_key":   "alpha",
			"worktree_path": worktreePath,
			"run_id":        "11",
			"run_attempt":   "1",
		},
	})
	if err != nil {
		t.Fatalf("RunTask(first) error = %v", err)
	}
	second, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "say ready again",
		Metadata: map[string]string{
			"project_key":   "alpha",
			"worktree_path": worktreePath,
			"run_id":        "12",
			"run_attempt":   "2",
		},
	})
	if err != nil {
		t.Fatalf("RunTask(second) error = %v", err)
	}

	if first.Metadata["artifact_path"] == second.Metadata["artifact_path"] {
		t.Fatalf("artifact paths = %q and %q, want distinct run-scoped paths", first.Metadata["artifact_path"], second.Metadata["artifact_path"])
	}
}

func TestHeadlessRunTaskWritesDurableArtifactOutsideLeasedWorktree(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER_MODE", "fixture")

	runtimeRoot := t.TempDir()
	worktreePath := filepath.Join(t.TempDir(), "leased-worktree")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(worktree) error = %v", err)
	}

	executor := NewHeadless()
	result, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "say ready",
		Metadata: map[string]string{
			"project_key":   "alpha",
			"runtime_root":  runtimeRoot,
			"worktree_path": worktreePath,
		},
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}

	artifactPath := result.Metadata["artifact_path"]
	if artifactPath == "" {
		t.Fatal("artifact_path empty, want persisted driver artifact")
	}
	if rel, err := filepath.Rel(runtimeRoot, artifactPath); err != nil {
		t.Fatalf("filepath.Rel(runtime_root, artifact_path) error = %v", err)
	} else if strings.HasPrefix(rel, "..") {
		t.Fatalf("artifact_path = %q, want under runtime_root %q", artifactPath, runtimeRoot)
	}
	if rel, err := filepath.Rel(worktreePath, artifactPath); err != nil {
		t.Fatalf("filepath.Rel(worktree_path, artifact_path) error = %v", err)
	} else if !strings.HasPrefix(rel, "..") {
		t.Fatalf("artifact_path = %q, want outside leased worktree %q", artifactPath, worktreePath)
	}

	if err := os.RemoveAll(worktreePath); err != nil {
		t.Fatalf("RemoveAll(worktree) error = %v", err)
	}

	content, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("ReadFile(artifact_path) after worktree cleanup error = %v", err)
	}
	if !strings.Contains(string(content), "runtime-smoke") {
		t.Fatalf("artifact content = %q, want task id runtime-smoke", string(content))
	}
}
