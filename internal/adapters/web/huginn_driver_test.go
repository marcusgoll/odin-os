package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDriverInvokesConfiguredCommandAndDecodesStructuredJSON(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_pbs_session","summary":"Validated trusted browser session state for the 2026-05 PBS workflow.","artifacts":{"bid_period":"2026-05","workflow_key":"pbs_may_bid","session_state":"ready","session_id":"huginn-session-1842","evidence":["session_alive","window_open","credentials_valid"]}}'
`)
	t.Setenv("ODIN_HUGINN_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	driver := NewDriver()
	response, err := driver.Invoke(context.Background(), Request{
		ToolKey: "browser_pbs_session",
		Input: Input{
			BidPeriod:   "2026-05",
			WorkflowKey: "pbs_may_bid",
			Timezone:    "America/Chicago",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.ToolKey != "browser_pbs_session" {
		t.Fatalf("ToolKey = %q, want browser_pbs_session", response.ToolKey)
	}
	if response.Summary != "Validated trusted browser session state for the 2026-05 PBS workflow." {
		t.Fatalf("Summary = %q, want fixture summary", response.Summary)
	}

	evidence, ok := response.Artifacts["evidence"].([]any)
	if !ok || len(evidence) != 3 {
		t.Fatalf("Artifacts.evidence = %#v, want 3 items", response.Artifacts["evidence"])
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request Request
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.ToolKey != "browser_pbs_session" {
		t.Fatalf("Request.ToolKey = %q, want browser_pbs_session", request.ToolKey)
	}
	if request.Input.WorkflowKey != "pbs_may_bid" {
		t.Fatalf("Request.Input.WorkflowKey = %q, want pbs_may_bid", request.Input.WorkflowKey)
	}
}

func TestDriverFailsClosedWithoutCommand(t *testing.T) {
	t.Setenv("ODIN_HUGINN_DRIVER", "")

	driver := NewDriver()
	if _, err := driver.Invoke(context.Background(), Request{ToolKey: "browser_pbs_session"}); err == nil {
		t.Fatal("Invoke() error = nil, want missing driver config failure")
	}
}

func TestDriverFailsClosedOnNonCompletedStatus(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"partial","tool_key":"browser_pbs_session","summary":"driver incomplete","artifacts":{"session_state":"pending"}}'
`)
	t.Setenv("ODIN_HUGINN_DRIVER", script)

	driver := NewDriver()
	if _, err := driver.Invoke(context.Background(), Request{
		ToolKey: "browser_pbs_session",
		Input: Input{
			BidPeriod:   "2026-05",
			WorkflowKey: "pbs_may_bid",
			Timezone:    "America/Chicago",
		},
	}); err == nil {
		t.Fatal("Invoke() error = nil, want non-completed status failure")
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
