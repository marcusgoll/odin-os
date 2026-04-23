package invocation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	caldriver "odin-os/internal/adapters/calendar"
	webdriver "odin-os/internal/adapters/web"
)

func TestServiceExposesRealCalendarResults(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "calendar-request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"google_calendar_off_dates","summary":"Found 2 off-dates for 2026-05.","artifacts":{"bid_period":"2026-05","calendar_id":"primary","timezone":"America/Chicago","off_dates":["2026-05-03","2026-05-04"]}}'
`)
	t.Setenv("ODIN_GOOGLE_CALENDAR_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	service := Service{}
	result, err := service.GoogleCalendarOffDates(context.Background(), caldriver.Request{
		ToolKey: "google_calendar_off_dates",
		Input: caldriver.Input{
			BidPeriod:  "2026-05",
			CalendarID: "primary",
			Timezone:   "America/Chicago",
		},
	})
	if err != nil {
		t.Fatalf("GoogleCalendarOffDates() error = %v", err)
	}
	if result.ToolKey != "google_calendar_off_dates" {
		t.Fatalf("ToolKey = %q, want google_calendar_off_dates", result.ToolKey)
	}
	if result.Summary != "Found 2 off-dates for 2026-05." {
		t.Fatalf("Summary = %q, want fixture summary", result.Summary)
	}
	if result.Artifacts["bid_period"] != "2026-05" {
		t.Fatalf("Artifacts.bid_period = %#v, want 2026-05", result.Artifacts["bid_period"])
	}
	if result.RawOutput == "" {
		t.Fatal("RawOutput = empty, want driver output")
	}
}

func TestServiceExposesRealHuginnResults(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "huginn-request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_pbs_session","summary":"Validated trusted browser session state for the 2026-05 PBS workflow.","artifacts":{"bid_period":"2026-05","workflow_key":"pbs_may_bid","session_state":"ready","session_id":"huginn-session-1842","evidence":["session_alive","window_open","credentials_valid"]}}'
`)
	t.Setenv("ODIN_HUGINN_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	service := Service{}
	result, err := service.HuginnPBSSession(context.Background(), webdriver.Request{
		ToolKey: "browser_pbs_session",
		Input: webdriver.Input{
			BidPeriod:   "2026-05",
			WorkflowKey: "pbs_may_bid",
			Timezone:    "America/Chicago",
		},
	})
	if err != nil {
		t.Fatalf("HuginnPBSSession() error = %v", err)
	}
	if result.ToolKey != "browser_pbs_session" {
		t.Fatalf("ToolKey = %q, want browser_pbs_session", result.ToolKey)
	}
	if result.Summary != "Validated trusted browser session state for the 2026-05 PBS workflow." {
		t.Fatalf("Summary = %q, want fixture summary", result.Summary)
	}
	if result.Artifacts["session_state"] != "ready" {
		t.Fatalf("Artifacts.session_state = %#v, want ready", result.Artifacts["session_state"])
	}
	if result.RawOutput == "" {
		t.Fatal("RawOutput = empty, want driver output")
	}
}

func TestServiceExposesRealHuginnVisualAuditResults(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "huginn-visual-request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_visual_audit","summary":"Captured browser visual audit evidence for cfipros-dashboard.","artifacts":{"target_url":"https://example.com/dashboard","final_url":"https://example.com/dashboard","title":"Dashboard","label":"cfipros-dashboard","screenshot_path":"/tmp/dashboard.png","snapshot_excerpt":"Revenue MRR Pipeline","wait_ms":"2000","launch_mode":"--headless"}}'
`)
	t.Setenv("ODIN_HUGINN_VISUAL_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	service := Service{}
	result, err := service.HuginnVisualAudit(context.Background(), webdriver.VisualRequest{
		ToolKey: "browser_visual_audit",
		Input: webdriver.VisualInput{
			TargetURL: "https://example.com/dashboard",
			Label:     "cfipros-dashboard",
			WaitMS:    "2000",
		},
	})
	if err != nil {
		t.Fatalf("HuginnVisualAudit() error = %v", err)
	}
	if result.ToolKey != "browser_visual_audit" {
		t.Fatalf("ToolKey = %q, want browser_visual_audit", result.ToolKey)
	}
	if result.Artifacts["screenshot_path"] != "/tmp/dashboard.png" {
		t.Fatalf("Artifacts.screenshot_path = %#v, want /tmp/dashboard.png", result.Artifacts["screenshot_path"])
	}
}

func TestServiceExposesRealHuginnXPostVisibleEvidenceResults(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "huginn-x-post-request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_x_post_visible_evidence","summary":"Captured visible X post evidence for marcus-crosswind.","artifacts":{"target_url":"https://x.com/marcus/status/123","final_url":"https://x.com/marcus/status/123","label":"marcus-crosswind","title":"X","screenshot_path":"/tmp/marcus-crosswind.png","snapshot_path":"/tmp/marcus-crosswind.txt","snapshot_excerpt":"Students don\\u0027t need more motivation","post_text":"Students don\\u0027t need more motivation","author_display_name":"Marcus Gollahon","author_handle":"@marcus","reply_count":"4","repost_count":"2","like_count":"18","view_count":"1400"}}'
`)
	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	service := Service{}
	result, err := service.HuginnXPostVisibleEvidence(context.Background(), webdriver.XPostRequest{
		ToolKey: "browser_x_post_visible_evidence",
		Input: webdriver.XPostInput{
			TargetURL: "https://x.com/marcus/status/123",
			Label:     "marcus-crosswind",
			WaitMS:    "2000",
		},
	})
	if err != nil {
		t.Fatalf("HuginnXPostVisibleEvidence() error = %v", err)
	}
	if result.ToolKey != "browser_x_post_visible_evidence" {
		t.Fatalf("ToolKey = %q, want browser_x_post_visible_evidence", result.ToolKey)
	}
	if result.Artifacts["snapshot_path"] != "/tmp/marcus-crosswind.txt" {
		t.Fatalf("Artifacts.snapshot_path = %#v, want /tmp/marcus-crosswind.txt", result.Artifacts["snapshot_path"])
	}
	if result.RawOutput == "" {
		t.Fatal("RawOutput = empty, want driver output")
	}
}

func TestServiceExposesRealHuginnXPostPublishResults(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "huginn-x-post-publish-request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_x_post_publish","summary":"Published approved X post through Browser Control.","artifacts":{"publish_url":"https://x.com/marcus/status/999999999","final_url":"https://x.com/marcus/status/999999999","label":"social-outcome-2","title":"X","screenshot_path":"/tmp/marcus-native-post.png","published_at":"2026-04-20T12:34:56Z","posted_text":"Approved X post ready to publish natively."}}'
`)
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	service := Service{}
	result, err := service.HuginnXPostPublish(context.Background(), webdriver.XPublishRequest{
		ToolKey: "browser_x_post_publish",
		Input: webdriver.XPublishInput{
			PostText: "Approved X post ready to publish natively.",
			Label:    "social-outcome-2",
			WaitMS:   "4000",
			Headless: "false",
		},
	})
	if err != nil {
		t.Fatalf("HuginnXPostPublish() error = %v", err)
	}
	if result.ToolKey != "browser_x_post_publish" {
		t.Fatalf("ToolKey = %q, want browser_x_post_publish", result.ToolKey)
	}
	if result.Artifacts["publish_url"] != "https://x.com/marcus/status/999999999" {
		t.Fatalf("Artifacts.publish_url = %#v, want expected URL", result.Artifacts["publish_url"])
	}
	if result.RawOutput == "" {
		t.Fatal("RawOutput = empty, want driver output")
	}
}

func TestServiceExposesReplyModeXPublishResults(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "huginn-x-post-publish-request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_x_post_publish","summary":"Published approved X reply through Browser Control.","artifacts":{"publish_url":"https://x.com/marcus/status/888888888","final_url":"https://x.com/marcus/status/888888888","label":"social-outcome-9","title":"X","screenshot_path":"/tmp/marcus-native-reply.png","published_at":"2026-04-20T12:34:56Z","posted_text":"Short, useful reply text.","in_reply_to_url":"https://x.com/example/status/123"}}'
`)
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	service := Service{}
	result, err := service.HuginnXPostPublish(context.Background(), webdriver.XPublishRequest{
		ToolKey: "browser_x_post_publish",
		Input: webdriver.XPublishInput{
			PostText:     "Short, useful reply text.",
			ContentKind:  "reply",
			InReplyToURL: "https://x.com/example/status/123",
			Label:        "social-outcome-9",
			WaitMS:       "4000",
			Headless:     "false",
		},
	})
	if err != nil {
		t.Fatalf("HuginnXPostPublish() error = %v", err)
	}
	if result.Artifacts["in_reply_to_url"] != "https://x.com/example/status/123" {
		t.Fatalf("Artifacts.in_reply_to_url = %#v, want expected target", result.Artifacts["in_reply_to_url"])
	}
	if result.RawOutput == "" {
		t.Fatal("RawOutput = empty, want driver output")
	}
}

func TestServiceFailsClosedWithoutDriverConfig(t *testing.T) {
	t.Setenv("ODIN_GOOGLE_CALENDAR_DRIVER", "")
	t.Setenv("ODIN_HUGINN_DRIVER", "")
	t.Setenv("ODIN_HUGINN_VISUAL_DRIVER", "")
	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", "")
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", "")

	service := Service{}
	if _, err := service.GoogleCalendarOffDates(context.Background(), caldriver.Request{ToolKey: "google_calendar_off_dates"}); err == nil {
		t.Fatal("GoogleCalendarOffDates() error = nil, want missing driver config failure")
	}
	if _, err := service.HuginnPBSSession(context.Background(), webdriver.Request{ToolKey: "browser_pbs_session"}); err == nil {
		t.Fatal("HuginnPBSSession() error = nil, want missing driver config failure")
	}
	if _, err := service.HuginnVisualAudit(context.Background(), webdriver.VisualRequest{ToolKey: "browser_visual_audit"}); err == nil {
		t.Fatal("HuginnVisualAudit() error = nil, want missing driver config failure")
	}
	if _, err := service.HuginnXPostVisibleEvidence(context.Background(), webdriver.XPostRequest{ToolKey: "browser_x_post_visible_evidence"}); err == nil {
		t.Fatal("HuginnXPostVisibleEvidence() error = nil, want missing driver config failure")
	}
	if _, err := service.HuginnXPostPublish(context.Background(), webdriver.XPublishRequest{ToolKey: "browser_x_post_publish"}); err == nil {
		t.Fatal("HuginnXPostPublish() error = nil, want missing driver config failure")
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
