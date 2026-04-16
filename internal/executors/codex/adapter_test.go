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
	driverPath := fixtureDriverPath(t)
	tracePath := filepath.Join(t.TempDir(), "health-trace.json")

	originalDriver := os.Getenv("ODIN_CODEX_DRIVER")
	originalTrace := os.Getenv("ODIN_CODEX_DRIVER_TRACE")
	if err := os.Setenv("ODIN_CODEX_DRIVER", driverPath); err != nil {
		t.Fatalf("Setenv(driver) error = %v", err)
	}
	if err := os.Setenv("ODIN_CODEX_DRIVER_TRACE", tracePath); err != nil {
		t.Fatalf("Setenv(trace) error = %v", err)
	}
	t.Cleanup(func() {
		if originalDriver == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER")
		} else {
			_ = os.Setenv("ODIN_CODEX_DRIVER", originalDriver)
		}
		if originalTrace == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER_TRACE")
		} else {
			_ = os.Setenv("ODIN_CODEX_DRIVER_TRACE", originalTrace)
		}
	})

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

func TestHeadlessHealthRejectsInvalidDriverStatus(t *testing.T) {
	driverPath := writeConfigurableDriver(t)
	tracePath := filepath.Join(t.TempDir(), "health-trace.json")

	originalDriver := os.Getenv("ODIN_CODEX_DRIVER")
	originalTrace := os.Getenv("ODIN_CODEX_DRIVER_TRACE")
	originalHealth := os.Getenv("ODIN_CODEX_DRIVER_HEALTH_RESPONSE")
	if err := os.Setenv("ODIN_CODEX_DRIVER", driverPath); err != nil {
		t.Fatalf("Setenv(driver) error = %v", err)
	}
	if err := os.Setenv("ODIN_CODEX_DRIVER_TRACE", tracePath); err != nil {
		t.Fatalf("Setenv(trace) error = %v", err)
	}
	if err := os.Setenv("ODIN_CODEX_DRIVER_HEALTH_RESPONSE", `{"status":"mystery","details":"broken"}`); err != nil {
		t.Fatalf("Setenv(health response) error = %v", err)
	}
	t.Cleanup(func() {
		if originalDriver == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER")
		} else {
			_ = os.Setenv("ODIN_CODEX_DRIVER", originalDriver)
		}
		if originalTrace == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER_TRACE")
		} else {
			_ = os.Setenv("ODIN_CODEX_DRIVER_TRACE", originalTrace)
		}
		if originalHealth == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER_HEALTH_RESPONSE")
		} else {
			_ = os.Setenv("ODIN_CODEX_DRIVER_HEALTH_RESPONSE", originalHealth)
		}
	})

	health, err := NewHeadless().Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != contract.HealthStatusUnavailable {
		t.Fatalf("Health().Status = %q, want %q", health.Status, contract.HealthStatusUnavailable)
	}
	if !strings.Contains(health.Details, "invalid health status") {
		t.Fatalf("Health().Details = %q, want invalid-status diagnostic", health.Details)
	}
}

func TestHeadlessRunTaskInvokesJsonDriver(t *testing.T) {
	driverPath := fixtureDriverPath(t)
	tracePath := filepath.Join(t.TempDir(), "trace.json")

	originalDriver := os.Getenv("ODIN_CODEX_DRIVER")
	originalTrace := os.Getenv("ODIN_CODEX_DRIVER_TRACE")
	if err := os.Setenv("ODIN_CODEX_DRIVER", driverPath); err != nil {
		t.Fatalf("Setenv(driver) error = %v", err)
	}
	if err := os.Setenv("ODIN_CODEX_DRIVER_TRACE", tracePath); err != nil {
		t.Fatalf("Setenv(trace) error = %v", err)
	}
	t.Cleanup(func() {
		if originalDriver == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER")
		} else {
			_ = os.Setenv("ODIN_CODEX_DRIVER", originalDriver)
		}
		if originalTrace == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER_TRACE")
		} else {
			_ = os.Setenv("ODIN_CODEX_DRIVER_TRACE", originalTrace)
		}
	})

	executor := NewHeadless()
	result, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     "task-123",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "summarize the change",
		Metadata: map[string]string{
			"project_key": "alpha",
		},
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}

	if result.Status != "completed" {
		t.Fatalf("RunTask().Status = %q, want completed", result.Status)
	}
	if result.Output != "fixture codex driver" {
		t.Fatalf("RunTask().Output = %q, want fixture output", result.Output)
	}
	if result.Handle.ExecutorKey != "codex_headless" {
		t.Fatalf("RunTask().Handle.ExecutorKey = %q, want codex_headless", result.Handle.ExecutorKey)
	}
	if result.Handle.ExternalID != "task-123" {
		t.Fatalf("RunTask().Handle.ExternalID = %q, want task-123", result.Handle.ExternalID)
	}

	trace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(trace) error = %v", err)
	}
	var request map[string]any
	if err := json.Unmarshal(trace, &request); err != nil {
		t.Fatalf("Unmarshal(trace) error = %v", err)
	}
	if got := request["action"]; got != "run" {
		t.Fatalf("request action = %v, want run", got)
	}
	task, ok := request["task"].(map[string]any)
	if !ok {
		t.Fatalf("request task missing: %#v", request)
	}
	if got := task["id"]; got != "task-123" {
		t.Fatalf("request task id = %v, want task-123", got)
	}
	if got := task["prompt"]; got != "summarize the change" {
		t.Fatalf("request task prompt = %v, want summarize the change", got)
	}
}

func TestHeadlessRunTaskRejectsEmptyDriverStatus(t *testing.T) {
	driverPath := writeConfigurableDriver(t)
	tracePath := filepath.Join(t.TempDir(), "run-trace.json")

	originalDriver := os.Getenv("ODIN_CODEX_DRIVER")
	originalTrace := os.Getenv("ODIN_CODEX_DRIVER_TRACE")
	originalRun := os.Getenv("ODIN_CODEX_DRIVER_RUN_RESPONSE")
	if err := os.Setenv("ODIN_CODEX_DRIVER", driverPath); err != nil {
		t.Fatalf("Setenv(driver) error = %v", err)
	}
	if err := os.Setenv("ODIN_CODEX_DRIVER_TRACE", tracePath); err != nil {
		t.Fatalf("Setenv(trace) error = %v", err)
	}
	if err := os.Setenv("ODIN_CODEX_DRIVER_RUN_RESPONSE", `{"status":"","output":"ignored"}`); err != nil {
		t.Fatalf("Setenv(run response) error = %v", err)
	}
	t.Cleanup(func() {
		if originalDriver == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER")
		} else {
			_ = os.Setenv("ODIN_CODEX_DRIVER", originalDriver)
		}
		if originalTrace == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER_TRACE")
		} else {
			_ = os.Setenv("ODIN_CODEX_DRIVER_TRACE", originalTrace)
		}
		if originalRun == "" {
			_ = os.Unsetenv("ODIN_CODEX_DRIVER_RUN_RESPONSE")
		} else {
			_ = os.Setenv("ODIN_CODEX_DRIVER_RUN_RESPONSE", originalRun)
		}
	})

	_, err := NewHeadless().RunTask(context.Background(), contract.TaskSpec{
		ID:     "task-123",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "summarize the change",
	})
	if err == nil {
		t.Fatal("RunTask() error = nil, want invalid run status failure")
	}
	if !strings.Contains(err.Error(), "invalid run status") {
		t.Fatalf("RunTask() error = %v, want invalid run status", err)
	}
}

func fixtureDriverPath(t *testing.T) string {
	t.Helper()

	return filepath.Clean(filepath.Join("..", "..", "..", "scripts", "drivers", "codex-headless.sh"))
}

func writeConfigurableDriver(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "codex-driver.sh")
	script := `#!/usr/bin/env bash
set -euo pipefail
payload="$(cat)"
if [[ -n "${ODIN_CODEX_DRIVER_TRACE:-}" ]]; then
	printf '%s\n' "$payload" >"${ODIN_CODEX_DRIVER_TRACE}"
fi
PAYLOAD="$payload" python3 - <<'PY'
import json
import os

request = json.loads(os.environ["PAYLOAD"])
action = request.get("action")
health = os.environ.get("ODIN_CODEX_DRIVER_HEALTH_RESPONSE", '{"status":"healthy","details":"fixture codex driver healthy"}')
run = os.environ.get("ODIN_CODEX_DRIVER_RUN_RESPONSE", '{"status":"completed","output":"fixture codex driver"}')

if action == "health":
    print(health)
elif action == "run":
    print(run)
else:
    print(json.dumps({"status":"unavailable","details":f"unknown action: {action}"}))
PY
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	return path
}
