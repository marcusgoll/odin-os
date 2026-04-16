package invocation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/adapters/browserhuman"
)

func TestServicePreservesStructuredArtifacts(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"huginn_browser_session","summary":"ok","artifacts":{"session_state":"ready","snapshots":[{"name":"home","url":"https://example.com"}],"labels":["alpha","beta"]},"debug_note":"preserve-me"}'
`)
	t.Setenv("ODIN_BROWSER_HUMAN_DRIVER", script)

	service := Service{}
	result, err := service.BrowserHuman(context.Background(), browserhuman.Request{
		ToolKey: "huginn_browser_session",
		Input:   map[string]any{"url": "https://example.com"},
	})
	if err != nil {
		t.Fatalf("BrowserHuman() error = %v", err)
	}
	if result.ToolKey != "huginn_browser_session" {
		t.Fatalf("ToolKey = %q, want huginn_browser_session", result.ToolKey)
	}
	if result.RawOutput != `{"status":"completed","tool_key":"huginn_browser_session","summary":"ok","artifacts":{"session_state":"ready","snapshots":[{"name":"home","url":"https://example.com"}],"labels":["alpha","beta"]},"debug_note":"preserve-me"}` {
		t.Fatalf("RawOutput = %q, want exact driver stdout", result.RawOutput)
	}
	if got := result.Artifacts["session_state"]; got != "ready" {
		t.Fatalf("Artifacts.session_state = %#v, want ready", got)
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
