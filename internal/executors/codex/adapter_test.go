package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	t.Setenv("ODIN_CODEX_DRIVER", "")
	t.Setenv("ODIN_CODEX_DRIVER_TRACE", tracePath)

	health, err := NewHeadlessWithRepoRoot(fixtureRepoRoot()).Health(context.Background())
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

func TestHeadlessHealthRequiresExplicitDriverHealthContract(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "called")
	driverPath := writeExecutable(t, "legacy-driver.sh", `#!/usr/bin/env bash
echo called > `+shellQuote(tracePath)+`
exit 1
`)
	t.Setenv("ODIN_CODEX_DRIVER", driverPath)

	health, err := NewHeadless().Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != contract.HealthStatusUnavailable {
		t.Fatalf("Health().Status = %q, want %q", health.Status, contract.HealthStatusUnavailable)
	}
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("legacy driver probe trace missing: %v", err)
	}
}

func TestHeadlessHealthFailsClosedForExplicitDriverProbeErrors(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{
			name: "failed health command",
			script: `#!/usr/bin/env bash
echo "codex CLI is not logged in" >&2
exit 1
`,
			want: "codex CLI is not logged in",
		},
		{
			name: "invalid json",
			script: `#!/usr/bin/env bash
printf 'not-json'
`,
			want: "invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driverPath := writeExecutable(t, "driver.sh", tt.script)
			t.Setenv("ODIN_CODEX_DRIVER", driverPath)

			health, err := NewHeadless().Health(context.Background())
			if err != nil {
				t.Fatalf("Health() error = %v", err)
			}
			if health.Status != contract.HealthStatusUnavailable {
				t.Fatalf("Health().Status = %q, want %q", health.Status, contract.HealthStatusUnavailable)
			}
			if !strings.Contains(health.Details, tt.want) {
				t.Fatalf("Health().Details = %q, want to contain %q", health.Details, tt.want)
			}
		})
	}
}

func TestHeadlessRunTaskUsesDriverScript(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")

	executor := NewHeadlessWithRepoRoot(fixtureRepoRoot())
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

func TestDriverRunTimeoutAllowsRealCodexSessions(t *testing.T) {
	if got := driverTimeout("run"); got < 30*time.Minute {
		t.Fatalf("driverTimeout(run) = %s, want at least 30m for live codex sessions", got)
	}
	if got := driverTimeout("health"); got >= driverTimeout("run") {
		t.Fatalf("driverTimeout(health) = %s, want shorter than run timeout %s", got, driverTimeout("run"))
	}
}

func TestHeadlessRunTaskTreatsExactCommandTextAsPromptData(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")

	markerPath := filepath.Join(t.TempDir(), "shell-command-ran.txt")
	executor := NewHeadlessWithRepoRoot(fixtureRepoRoot())
	result, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "run this exact command: printf 'operator visible failure proof' > " + markerPath,
		Metadata: map[string]string{
			"project_key": "alpha",
			"repo_root":   fixtureRepoRoot(),
		},
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if result.Output != "fixture codex driver" {
		t.Fatalf("Output = %q, want fixture driver output", result.Output)
	}
	if result.Metadata["driver"] != "codex_headless_script" {
		t.Fatalf("driver metadata = %q, want codex_headless_script", result.Metadata["driver"])
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("shell marker exists, want prompt text not executed as shell")
	}
}

func TestHeadlessRunTaskUsesLegacyDriverWhenExplicitDriverConfigured(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "legacy-request.json")
	driverPath := writeExecutable(t, "legacy-driver.sh", `#!/usr/bin/env bash
set -euo pipefail
cat > `+shellQuote(tracePath)+`
if [[ "${ODIN_CODEX_DRIVER_ACTION:-}" != "run" ]]; then
  echo "missing legacy action" >&2
  exit 1
fi
python3 - `+shellQuote(tracePath)+` <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    request = json.load(handle)
if request.get("prompt") != "say ready":
    raise SystemExit("missing top-level prompt")
if "action" in request:
    raise SystemExit("legacy request should not include action")
json.dump({
    "status": "completed",
    "output": "legacy ready",
    "metadata": {"driver": "legacy"},
}, sys.stdout)
PY
`)
	t.Setenv("ODIN_CODEX_DRIVER", driverPath)

	result, err := NewHeadless().RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "say ready",
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if result.Output != "legacy ready" {
		t.Fatalf("Output = %q, want legacy ready", result.Output)
	}
	if result.Metadata["driver"] != "legacy" {
		t.Fatalf("driver metadata = %q, want legacy", result.Metadata["driver"])
	}

	trace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(trace) error = %v", err)
	}
	var request map[string]any
	if err := json.Unmarshal(trace, &request); err != nil {
		t.Fatalf("Unmarshal(trace) error = %v", err)
	}
	if got := request["prompt"]; got != "say ready" {
		t.Fatalf("legacy request prompt = %v, want say ready", got)
	}
}

func TestHeadlessRunTaskLegacyDriverUsesAllowlistedEnvironment(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "legacy-env.txt")
	driverPath := writeExecutable(t, "legacy-env-driver.sh", `#!/usr/bin/env bash
set -euo pipefail
env > `+shellQuote(tracePath)+`
if [[ "${ODIN_CODEX_DRIVER_ACTION:-}" != "run" ]]; then
  echo "missing legacy action" >&2
  exit 1
fi
printf '{"status":"completed","output":"legacy ready"}'
`)
	t.Setenv("ODIN_CODEX_DRIVER", driverPath)
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("OPENAI_API_KEY", "sk-secret")
	t.Setenv("ODIN_ADMIN_TOKEN", "admin-secret")

	if _, err := NewHeadless().RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "say ready",
	}); err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}

	envBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(trace) error = %v", err)
	}
	env := string(envBytes)
	for _, forbidden := range []string{"GITHUB_TOKEN=", "OPENAI_API_KEY=", "ODIN_ADMIN_TOKEN=", "ghp_secret", "sk-secret", "admin-secret"} {
		if strings.Contains(env, forbidden) {
			t.Fatalf("legacy driver env contains forbidden value %q in:\n%s", forbidden, env)
		}
	}
	if !strings.Contains(env, "ODIN_CODEX_DRIVER_ACTION=run") {
		t.Fatalf("legacy driver env missing action in:\n%s", env)
	}
}

func TestHeadlessRunTaskRejectsEmptyDriverStatus(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")
	t.Setenv("ODIN_CODEX_DRIVER_RUN_RESPONSE", `{"status":"","output":"ignored"}`)

	_, err := NewHeadlessWithRepoRoot(fixtureRepoRoot()).RunTask(context.Background(), contract.TaskSpec{
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

func TestHeadlessRunTaskUsesConfiguredRunTimeout(t *testing.T) {
	driverPath := writeExecutable(t, "slow-driver.sh", `#!/usr/bin/env bash
sleep 2
printf '{"status":"completed","output":"late"}'
`)
	t.Setenv("ODIN_CODEX_DRIVER", driverPath)
	t.Setenv("ODIN_CODEX_DRIVER_RUN_TIMEOUT", "1s")

	_, err := NewHeadless().RunTask(context.Background(), contract.TaskSpec{
		ID:     "runtime-smoke",
		Kind:   contract.TaskKindGeneral,
		Scope:  "project",
		Prompt: "say ready",
	})
	if err == nil {
		t.Fatal("RunTask() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "codex driver timed out after 1s") {
		t.Fatalf("RunTask() error = %v, want configured 1s timeout", err)
	}
}

func TestHeadlessRunTaskWritesArtifactMetadata(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")

	worktreePath := t.TempDir()
	executor := NewHeadlessWithRepoRoot(fixtureRepoRoot())
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

func TestHeadlessRunTaskBoundsLongArtifactFilename(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER", "")

	worktreePath := t.TempDir()
	longID := strings.Repeat("very-long-task-key-", 40)
	executor := NewHeadlessWithRepoRoot(fixtureRepoRoot())
	result, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:     longID,
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
	base := filepath.Base(artifactPath)
	if len(base) > 180 {
		t.Fatalf("artifact basename length = %d, want <= 180: %s", len(base), base)
	}
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("artifact path stat error = %v", err)
	}
}

func fixtureRepoRoot() string {
	return filepath.Clean(filepath.Join("..", "..", ".."))
}

func writeExecutable(t *testing.T, name string, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", name, err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
