package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/executors/contract"
)

func TestDriverExecutorReturnsUnavailableWhenCommandMissing(t *testing.T) {
	t.Parallel()

	executor := NewDriver("codex_headless", "ODIN_CODEX_DRIVER", "codex")
	report, err := executor.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if report.Status != contract.HealthStatusUnavailable {
		t.Fatalf("Status = %q, want unavailable", report.Status)
	}
}

func TestDriverExecutorRunsFixtureProcess(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, requestPath, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","output":"driver ok","external_id":"fixture-1"}'
`)
	t.Setenv("ODIN_CODEX_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	executor := NewDriver("codex_headless", "ODIN_CODEX_DRIVER", "codex")
	result, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     "t-1",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if result.Output != "driver ok" {
		t.Fatalf("Output = %q, want driver ok", result.Output)
	}
	if result.Handle.ExternalID != "fixture-1" {
		t.Fatalf("ExternalID = %q, want fixture-1", result.Handle.ExternalID)
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request DriverRequest
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.ExecutorKey != "codex_headless" {
		t.Fatalf("ExecutorKey = %q, want codex_headless", request.ExecutorKey)
	}
	if request.Backend != "codex" {
		t.Fatalf("Backend = %q, want codex", request.Backend)
	}
	if request.Task.ID != "t-1" {
		t.Fatalf("Task.ID = %q, want t-1", request.Task.ID)
	}
}

func writeFixtureDriver(t *testing.T, requestPath string, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "driver.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	_ = requestPath
	return path
}
