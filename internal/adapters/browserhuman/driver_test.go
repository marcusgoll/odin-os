package browserhuman

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDriverFailsClosedWithoutCommand(t *testing.T) {
	t.Setenv(defaultDriverEnvVar, "")

	driver := NewDriver()
	if _, err := driver.Invoke(context.Background(), Request{}); err == nil {
		t.Fatal("Invoke() error = nil, want missing driver config failure")
	}
}

func TestDriverDefaultsEmptyToolKeyWhenAllowed(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"huginn_browser_session","summary":"ok","artifacts":{"session_state":"ready"}}'
`)
	t.Setenv(defaultDriverEnvVar, script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	driver := NewDriver()
	driver.DefaultToolKey = "huginn_browser_session"

	response, err := driver.Invoke(context.Background(), Request{
		AllowDefaultToolKey: true,
		Input: map[string]any{
			"url": "https://example.com",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.ToolKey != "huginn_browser_session" {
		t.Fatalf("ToolKey = %q, want huginn_browser_session", response.ToolKey)
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request Request
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.ToolKey != "huginn_browser_session" {
		t.Fatalf("Request.ToolKey = %q, want huginn_browser_session", request.ToolKey)
	}
}

func TestDriverRejectsMismatchedToolKey(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"other_tool","summary":"ok","artifacts":{"session_state":"ready"}}'
`)
	t.Setenv(defaultDriverEnvVar, script)

	driver := NewDriver()
	driver.DefaultToolKey = "huginn_browser_session"

	if _, err := driver.Invoke(context.Background(), Request{
		ToolKey: "huginn_browser_session",
		Input:   map[string]any{"url": "https://example.com"},
	}); err == nil {
		t.Fatal("Invoke() error = nil, want mismatched response tool key failure")
	}
}

func TestDriverRejectsNonCompletedStatus(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"failed","tool_key":"huginn_browser_session","summary":"not done","artifacts":{"session_state":"blocked"}}'
`)
	t.Setenv(defaultDriverEnvVar, script)

	driver := NewDriver()
	driver.DefaultToolKey = "huginn_browser_session"

	if _, err := driver.Invoke(context.Background(), Request{
		ToolKey: "huginn_browser_session",
		Input:   map[string]any{"url": "https://example.com"},
	}); err == nil {
		t.Fatal("Invoke() error = nil, want non-completed status failure")
	}
}

func TestDriverPreservesStructuredArtifacts(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"huginn_browser_session","summary":"ok","artifacts":{"session_state":"ready","snapshots":[{"name":"home","url":"https://example.com"}],"labels":["alpha","beta"]}}'
`)
	t.Setenv(defaultDriverEnvVar, script)

	driver := NewDriver()
	driver.DefaultToolKey = "huginn_browser_session"

	response, err := driver.Invoke(context.Background(), Request{
		ToolKey: "huginn_browser_session",
		Input:   map[string]any{"url": "https://example.com"},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}

	snapshots, ok := response.Artifacts["snapshots"].([]any)
	if !ok || len(snapshots) != 1 {
		t.Fatalf("Artifacts.snapshots = %#v, want 1 structured item", response.Artifacts["snapshots"])
	}
	snapshot, ok := snapshots[0].(map[string]any)
	if !ok {
		t.Fatalf("Artifacts.snapshots[0] = %#v, want structured object", snapshots[0])
	}
	if snapshot["name"] != "home" {
		t.Fatalf("Artifacts.snapshots[0].name = %#v, want home", snapshot["name"])
	}
	labels, ok := response.Artifacts["labels"].([]any)
	if !ok || len(labels) != 2 {
		t.Fatalf("Artifacts.labels = %#v, want 2 labels", response.Artifacts["labels"])
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
