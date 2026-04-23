package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVisualDriverInvokesConfiguredCommandAndDecodesStructuredJSON(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_visual_audit","summary":"Captured browser visual audit evidence for cfipros-dashboard.","artifacts":{"target_url":"https://example.com/dashboard","final_url":"https://example.com/dashboard","title":"Dashboard","label":"cfipros-dashboard","screenshot_path":"/tmp/dashboard.png","snapshot_excerpt":"Revenue MRR Pipeline","wait_ms":"2000","launch_mode":"--headless"}}'
`)
	t.Setenv("ODIN_HUGINN_VISUAL_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	driver := NewVisualDriver()
	response, err := driver.Invoke(context.Background(), VisualRequest{
		ToolKey: "browser_visual_audit",
		Input: VisualInput{
			TargetURL: "https://example.com/dashboard",
			Label:     "cfipros-dashboard",
			WaitMS:    "2000",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.ToolKey != "browser_visual_audit" {
		t.Fatalf("ToolKey = %q, want browser_visual_audit", response.ToolKey)
	}
	if response.Artifacts["screenshot_path"] != "/tmp/dashboard.png" {
		t.Fatalf("Artifacts.screenshot_path = %#v, want /tmp/dashboard.png", response.Artifacts["screenshot_path"])
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request VisualRequest
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.Input.TargetURL != "https://example.com/dashboard" {
		t.Fatalf("Request.Input.TargetURL = %q, want https://example.com/dashboard", request.Input.TargetURL)
	}
}

func TestVisualDriverFailsClosedWithoutCommand(t *testing.T) {
	t.Setenv("ODIN_HUGINN_VISUAL_DRIVER", "")

	driver := NewVisualDriver()
	if _, err := driver.Invoke(context.Background(), VisualRequest{ToolKey: "browser_visual_audit"}); err == nil {
		t.Fatal("Invoke() error = nil, want missing driver config failure")
	}
}

func TestVisualDriverFailsClosedOnNonCompletedStatus(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"partial","tool_key":"browser_visual_audit","summary":"driver incomplete","artifacts":{"label":"cfipros-dashboard"}}'
`)
	t.Setenv("ODIN_HUGINN_VISUAL_DRIVER", script)

	driver := NewVisualDriver()
	if _, err := driver.Invoke(context.Background(), VisualRequest{
		ToolKey: "browser_visual_audit",
		Input: VisualInput{
			TargetURL: "https://example.com/dashboard",
			Label:     "cfipros-dashboard",
		},
	}); err == nil {
		t.Fatal("Invoke() error = nil, want non-completed status failure")
	}
}
