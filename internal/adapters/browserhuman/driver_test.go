package browserhuman

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDriverFailsClosedWithoutCommand(t *testing.T) {
	t.Setenv(defaultDriverEnvVar, "")

	driver := NewDriver()
	if _, err := driver.Invoke(context.Background(), Request{}); err == nil {
		t.Fatal("Invoke() error = nil, want missing driver config failure")
	}
}

func TestDriverInvokesCommandWithSpaces(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "driver dir with spaces")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(workDir) error = %v", err)
	}

	scriptPath := filepath.Join(workDir, "driver script.sh")
	requestPath := filepath.Join(t.TempDir(), "request.json")
	argPath := filepath.Join(t.TempDir(), "argument.txt")
	script := `#!/usr/bin/env bash
printf '%s\n' "$1" >"$ODIN_DRIVER_ARG_PATH"
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"huginn_browser_session","summary":"spaces ok","artifacts":{"session_state":"ready"}}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(script) error = %v", err)
	}
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatalf("Chmod(script) error = %v", err)
	}

	t.Setenv(defaultDriverEnvVar, fmt.Sprintf("%q %q", scriptPath, "argument with spaces"))
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)
	t.Setenv("ODIN_DRIVER_ARG_PATH", argPath)

	driver := NewDriver()
	driver.DefaultToolKey = "huginn_browser_session"

	response, err := driver.Invoke(context.Background(), Request{
		ToolKey: "huginn_browser_session",
		Input: map[string]any{
			"url": "https://example.com",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.Summary != "spaces ok" {
		t.Fatalf("Summary = %q, want spaces ok", response.Summary)
	}

	argBytes, err := os.ReadFile(argPath)
	if err != nil {
		t.Fatalf("ReadFile(arg) error = %v", err)
	}
	if got := string(argBytes); got != "argument with spaces\n" {
		t.Fatalf("argument file = %q, want %q", got, "argument with spaces\n")
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

func TestDriverExecsConfiguredCommandForCancellation(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "driver.pid")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '%s' "$$" >"$ODIN_DRIVER_PID_PATH"
while :; do :; done
printf '{"status":"completed","tool_key":"huginn_browser_session","summary":"ok","artifacts":{"session_state":"ready"}}'
`)
	t.Setenv(defaultDriverEnvVar, script)
	t.Setenv("ODIN_DRIVER_PID_PATH", pidPath)

	driver := NewDriver()
	driver.DefaultToolKey = "huginn_browser_session"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := driver.Invoke(ctx, Request{
			ToolKey: "huginn_browser_session",
			Input:   map[string]any{"url": "https://example.com"},
		})
		done <- err
	}()

	pid := waitForPIDFile(t, pidPath)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Invoke() error = nil, want cancellation failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Invoke() did not return after cancellation")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if err != nil {
			t.Fatalf("Kill(%d, 0) error = %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d still exists after cancellation", pid)
		}
		time.Sleep(25 * time.Millisecond)
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

func TestDriverValidationErrorsIncludeRawStdout(t *testing.T) {
	t.Run("mismatched tool key", func(t *testing.T) {
		script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"other_tool","summary":"ok","artifacts":{"session_state":"ready"}}'
`)
		t.Setenv(defaultDriverEnvVar, script)

		driver := NewDriver()
		driver.DefaultToolKey = "huginn_browser_session"

		_, err := driver.Invoke(context.Background(), Request{
			ToolKey: "huginn_browser_session",
			Input:   map[string]any{"url": "https://example.com"},
		})
		if err == nil {
			t.Fatal("Invoke() error = nil, want mismatched response tool key failure")
		}
		if !strings.Contains(err.Error(), `stdout="{\"status\":\"completed\",\"tool_key\":\"other_tool\"`) {
			t.Fatalf("error = %v, want raw stdout payload", err)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf 'not-json'
`)
		t.Setenv(defaultDriverEnvVar, script)

		driver := NewDriver()
		driver.DefaultToolKey = "huginn_browser_session"

		_, err := driver.Invoke(context.Background(), Request{
			ToolKey: "huginn_browser_session",
			Input:   map[string]any{"url": "https://example.com"},
		})
		if err == nil {
			t.Fatal("Invoke() error = nil, want decode failure")
		}
		if !strings.Contains(err.Error(), `stdout="not-json"`) {
			t.Fatalf("error = %v, want raw stdout payload", err)
		}
	})
}

func TestDriverRejectsMissingResponseToolKey(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","summary":"ok","artifacts":{"session_state":"ready"}}'
`)
	t.Setenv(defaultDriverEnvVar, script)

	driver := NewDriver()
	driver.DefaultToolKey = "huginn_browser_session"

	if _, err := driver.Invoke(context.Background(), Request{
		ToolKey: "huginn_browser_session",
		Input:   map[string]any{"url": "https://example.com"},
	}); err == nil {
		t.Fatal("Invoke() error = nil, want missing response tool key failure")
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

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("parse pid file %q: %v", string(data), err)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid file %s was not written", path)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
