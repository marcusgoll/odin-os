package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/executors/contract"
)

func TestHeadlessHealthIsUnavailableWithoutDriver(t *testing.T) {
	original := os.Getenv("ODIN_CODEX_DRIVER")
	if err := os.Unsetenv("ODIN_CODEX_DRIVER"); err != nil {
		t.Fatalf("Unsetenv() error = %v", err)
	}
	t.Cleanup(func() {
		if original == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER")
			return
		}
		_ = os.Setenv("ODIN_CODEX_DRIVER", original)
	})

	health, err := NewHeadless().Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != contract.HealthStatusUnavailable {
		t.Fatalf("Health().Status = %q, want %q", health.Status, contract.HealthStatusUnavailable)
	}
}

func TestHeadlessCapabilitiesOnlyClaimImplementedFeatures(t *testing.T) {
	caps, err := NewHeadless().Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if !caps.SupportsHeadlessPlan {
		t.Fatal("SupportsHeadlessPlan = false, want true")
	}
	if caps.SupportsResume {
		t.Fatal("SupportsResume = true, want false")
	}
	if caps.SupportsCancel {
		t.Fatal("SupportsCancel = true, want false")
	}
	if caps.SupportsTools {
		t.Fatal("SupportsTools = true, want false")
	}
	if caps.SupportsCostEstimate {
		t.Fatal("SupportsCostEstimate = true, want false")
	}
}

func TestHeadlessHealthInvokesJsonDriver(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "health-trace.json")
	t.Setenv("ODIN_CODEX_DRIVER", fixtureDriverPath(t))
	t.Setenv("ODIN_CODEX_DRIVER_TRACE", tracePath)

	health, err := NewHeadless().Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != contract.HealthStatusHealthy {
		t.Fatalf("Health().Status = %q, want healthy", health.Status)
	}
	if health.Details != "fixture codex driver healthy" {
		t.Fatalf("Health().Details = %q, want fixture codex driver healthy", health.Details)
	}

	trace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(trace) error = %v", err)
	}
	var request map[string]any
	if err := json.Unmarshal(trace, &request); err != nil {
		t.Fatalf("Unmarshal(trace) error = %v", err)
	}
	if got := request["action"]; got != "health" {
		t.Fatalf("request action = %v, want health", got)
	}
}

func TestHeadlessRunTaskUsesDriverScript(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", fixtureDriverPath(t))

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
	if result.Output != "fixture codex driver" {
		t.Fatalf("Output = %q, want fixture codex driver", result.Output)
	}
	if result.Metadata["driver"] != "codex_headless_script" {
		t.Fatalf("driver metadata = %q, want codex_headless_script", result.Metadata["driver"])
	}
}

func TestHeadlessRunTaskRejectsEmptyDriverStatus(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", fixtureDriverPath(t))
	t.Setenv("ODIN_CODEX_DRIVER_RUN_RESPONSE", `{"status":"","output":"ignored"}`)

	_, err := NewHeadless().RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "say ready",
	})
	if err == nil {
		t.Fatal("RunTask() error = nil, want invalid status")
	}
	if !strings.Contains(err.Error(), "invalid run status") {
		t.Fatalf("RunTask() error = %v, want invalid run status", err)
	}
}

func TestHeadlessRunTaskWritesArtifactMetadata(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", fixtureDriverPath(t))

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

func fixtureDriverPath(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join("..", "..", "..", "scripts", "drivers", "codex-headless.sh"))
}
