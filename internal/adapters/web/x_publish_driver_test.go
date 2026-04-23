package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestXPublishDriverInvokesConfiguredCommandAndDecodesStructuredJSON(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_x_post_publish","summary":"Published approved X post through Browser Control.","artifacts":{"publish_url":"https://x.com/marcus/status/999999999","final_url":"https://x.com/marcus/status/999999999","label":"social-outcome-2","title":"X","screenshot_path":"/tmp/marcus-native-post.png","published_at":"2026-04-20T12:34:56Z","posted_text":"Approved X post ready to publish natively."}}'
`)
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	driver := NewXPublishDriver()
	response, err := driver.Invoke(context.Background(), XPublishRequest{
		ToolKey: "browser_x_post_publish",
		Input: XPublishInput{
			PostText: "Approved X post ready to publish natively.",
			Label:    "social-outcome-2",
			WaitMS:   "4000",
			Headless: "false",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.ToolKey != "browser_x_post_publish" {
		t.Fatalf("ToolKey = %q, want browser_x_post_publish", response.ToolKey)
	}
	if response.Artifacts["publish_url"] != "https://x.com/marcus/status/999999999" {
		t.Fatalf("Artifacts.publish_url = %#v, want https://x.com/marcus/status/999999999", response.Artifacts["publish_url"])
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request XPublishRequest
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.Input.PostText != "Approved X post ready to publish natively." {
		t.Fatalf("Request.Input.PostText = %q, want expected text", request.Input.PostText)
	}
}

func TestXPublishDriverPassesReplyModeFieldsThroughToDriver(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_x_post_publish","summary":"Published approved X reply through Browser Control.","artifacts":{"publish_url":"https://x.com/marcus/status/888888888","final_url":"https://x.com/marcus/status/888888888","label":"social-outcome-9","title":"X","screenshot_path":"/tmp/marcus-native-reply.png","published_at":"2026-04-20T12:34:56Z","posted_text":"Short, useful reply text.","in_reply_to_url":"https://x.com/example/status/123"}}'
`)
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	driver := NewXPublishDriver()
	response, err := driver.Invoke(context.Background(), XPublishRequest{
		ToolKey: "browser_x_post_publish",
		Input: XPublishInput{
			PostText:     "Short, useful reply text.",
			ContentKind:  "reply",
			InReplyToURL: "https://x.com/example/status/123",
			Label:        "social-outcome-9",
			WaitMS:       "4000",
			Headless:     "false",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.Artifacts["publish_url"] != "https://x.com/marcus/status/888888888" {
		t.Fatalf("Artifacts.publish_url = %#v, want expected reply url", response.Artifacts["publish_url"])
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request XPublishRequest
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.Input.ContentKind != "reply" {
		t.Fatalf("Request.Input.ContentKind = %q, want reply", request.Input.ContentKind)
	}
	if request.Input.InReplyToURL != "https://x.com/example/status/123" {
		t.Fatalf("Request.Input.InReplyToURL = %q, want expected target", request.Input.InReplyToURL)
	}
}

func TestXPublishDriverFailsClosedWithoutCommand(t *testing.T) {
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", "")

	driver := NewXPublishDriver()
	if _, err := driver.Invoke(context.Background(), XPublishRequest{ToolKey: "browser_x_post_publish"}); err == nil {
		t.Fatal("Invoke() error = nil, want missing driver config failure")
	}
}

func TestXPublishDriverFailsClosedOnNonCompletedStatus(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"failed","tool_key":"browser_x_post_publish","summary":"publish incomplete","artifacts":{"label":"social-outcome-2"}}'
`)
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", script)

	driver := NewXPublishDriver()
	if _, err := driver.Invoke(context.Background(), XPublishRequest{
		ToolKey: "browser_x_post_publish",
		Input: XPublishInput{
			PostText: "Approved X post ready to publish natively.",
			Label:    "social-outcome-2",
		},
	}); err == nil {
		t.Fatal("Invoke() error = nil, want non-completed status failure")
	}
}
