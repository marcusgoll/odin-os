package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestXPostDriverInvokesConfiguredCommandAndDecodesStructuredJSON(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_x_post_visible_evidence","summary":"Captured visible X post evidence for marcus-crosswind.","artifacts":{"target_url":"https://x.com/marcus/status/123","final_url":"https://x.com/marcus/status/123","label":"marcus-crosswind","title":"X","screenshot_path":"/tmp/marcus-crosswind.png","snapshot_path":"/tmp/marcus-crosswind.txt","snapshot_excerpt":"Students don\\u0027t need more motivation","post_text":"Students don\\u0027t need more motivation","author_display_name":"Marcus Gollahon","author_handle":"@marcus","reply_count":"4","repost_count":"2","like_count":"18","view_count":"1400"}}'
`)
	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	driver := NewXPostDriver()
	response, err := driver.Invoke(context.Background(), XPostRequest{
		ToolKey: "browser_x_post_visible_evidence",
		Input: XPostInput{
			TargetURL: "https://x.com/marcus/status/123",
			Label:     "marcus-crosswind",
			WaitMS:    "2000",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.ToolKey != "browser_x_post_visible_evidence" {
		t.Fatalf("ToolKey = %q, want browser_x_post_visible_evidence", response.ToolKey)
	}
	if response.Artifacts["snapshot_path"] != "/tmp/marcus-crosswind.txt" {
		t.Fatalf("Artifacts.snapshot_path = %#v, want /tmp/marcus-crosswind.txt", response.Artifacts["snapshot_path"])
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request XPostRequest
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.Input.TargetURL != "https://x.com/marcus/status/123" {
		t.Fatalf("Request.Input.TargetURL = %q, want https://x.com/marcus/status/123", request.Input.TargetURL)
	}
}

func TestXPostDriverFailsClosedWithoutCommand(t *testing.T) {
	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", "")

	driver := NewXPostDriver()
	if _, err := driver.Invoke(context.Background(), XPostRequest{ToolKey: "browser_x_post_visible_evidence"}); err == nil {
		t.Fatal("Invoke() error = nil, want missing driver config failure")
	}
}

func TestXPostDriverFailsClosedOnNonCompletedStatus(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"partial","tool_key":"browser_x_post_visible_evidence","summary":"driver incomplete","artifacts":{"label":"marcus-crosswind"}}'
`)
	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", script)

	driver := NewXPostDriver()
	if _, err := driver.Invoke(context.Background(), XPostRequest{
		ToolKey: "browser_x_post_visible_evidence",
		Input: XPostInput{
			TargetURL: "https://x.com/marcus/status/123",
			Label:     "marcus-crosswind",
		},
	}); err == nil {
		t.Fatal("Invoke() error = nil, want non-completed status failure")
	}
}
