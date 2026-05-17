package drivers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvokeUsesAllowlistedEnvironment(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "env.txt")
	driverPath := writeDriver(t, `#!/bin/sh
env > `+shellQuote(tracePath)+`
printf '{"status":"completed"}'
`)
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("OPENAI_API_KEY", "sk-secret")
	t.Setenv("ODIN_ADMIN_TOKEN", "admin-secret")
	t.Setenv("ODIN_CODEX_SANDBOX_MODE", "workspace-write")
	t.Setenv("ODIN_CODEX_HOST_DRIVER_SOCKET", "/tmp/odin-codex-driver.sock")
	t.Setenv("ODIN_ROOT", "/tmp/odin-root")

	var response struct {
		Status string `json:"status"`
	}
	if _, err := Invoke(context.Background(), Options{
		DriverPath: driverPath,
		Label:      "test",
	}, map[string]string{"action": "health"}, &response); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}

	envBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(trace) error = %v", err)
	}
	env := string(envBytes)
	for _, forbidden := range []string{"GITHUB_TOKEN=", "OPENAI_API_KEY=", "ODIN_ADMIN_TOKEN=", "ghp_secret", "sk-secret", "admin-secret"} {
		if strings.Contains(env, forbidden) {
			t.Fatalf("driver env contains forbidden value %q in:\n%s", forbidden, env)
		}
	}
	for _, required := range []string{"PATH=", "ODIN_CODEX_SANDBOX_MODE=workspace-write", "ODIN_CODEX_HOST_DRIVER_SOCKET=/tmp/odin-codex-driver.sock", "ODIN_ROOT=/tmp/odin-root"} {
		if !strings.Contains(env, required) {
			t.Fatalf("driver env missing required value %q in:\n%s", required, env)
		}
	}
}

func TestAllowlistedEnvironmentIsStableAndJSONSerializable(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("ODIN_ADMIN_TOKEN", "admin-secret")
	t.Setenv("ODIN_CODEX_SANDBOX_MODE", "read-only")
	t.Setenv("ODIN_CODEX_DRIVER_ACTION", "run")

	env := AllowlistedEnvironment()
	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal(env) error = %v", err)
	}
	for _, forbidden := range []string{"GITHUB_TOKEN", "ODIN_ADMIN_TOKEN", "ghp_secret", "admin-secret"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("allowlisted env contains forbidden value %q in %s", forbidden, encoded)
		}
	}
	if strings.Contains(string(encoded), "ODIN_CODEX_DRIVER_ACTION") {
		t.Fatalf("allowlisted env inherited explicit-only action in %s", encoded)
	}
	withAction := AllowlistedEnvironment("ODIN_CODEX_DRIVER_ACTION=run")
	if !strings.Contains(strings.Join(withAction, "\n"), "ODIN_CODEX_DRIVER_ACTION=run") {
		t.Fatalf("allowlisted env with explicit action = %#v, want action", withAction)
	}
}

func writeDriver(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "driver.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
