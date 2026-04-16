package claude_code

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/executors/contract"
)

func TestNewHeadlessUsesClaudeHarnessDriver(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","output":"claude ok","external_id":"fixture-claude"}'
`)
	t.Setenv("ODIN_CLAUDE_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	executor := NewHeadless()
	report, err := executor.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if report.Status != contract.HealthStatusHealthy {
		t.Fatalf("Health().Status = %q, want %q", report.Status, contract.HealthStatusHealthy)
	}

	result, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     "t-claude",
		Kind:   contract.TaskKindResearch,
		Scope:  "project",
		Prompt: "investigate the issue",
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if result.Output != "claude ok" {
		t.Fatalf("Output = %q, want claude ok", result.Output)
	}
	if result.Handle.ExternalID != "fixture-claude" {
		t.Fatalf("ExternalID = %q, want fixture-claude", result.Handle.ExternalID)
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request struct {
		ExecutorKey string            `json:"executor_key"`
		Backend     string            `json:"backend"`
		Task        contract.TaskSpec `json:"task"`
	}
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.ExecutorKey != "claude_code_headless" {
		t.Fatalf("ExecutorKey = %q, want claude_code_headless", request.ExecutorKey)
	}
	if request.Backend != "claude" {
		t.Fatalf("Backend = %q, want claude", request.Backend)
	}
	if request.Task.ID != "t-claude" {
		t.Fatalf("Task.ID = %q, want t-claude", request.Task.ID)
	}
}

func writeFixtureDriver(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "driver.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	return path
}
