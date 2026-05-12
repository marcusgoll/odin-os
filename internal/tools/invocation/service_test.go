package invocation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/adapters/browserhuman"
	caldriver "odin-os/internal/adapters/calendar"
	webdriver "odin-os/internal/adapters/web"
	"odin-os/internal/store/sqlite"
)

func TestBuiltinToolInvokesRuntimeDriver(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeRoot := t.TempDir()
	store := openInvocationStore(t, runtimeRoot)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       runtimeRoot,
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-queued",
		Title:       "Queued runtime task",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "test",
	}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	service := Service{RuntimeRoot: runtimeRoot}
	result, err := service.Invoke(ctx, "project_status", Request{
		Args: map[string]string{"project_key": "alpha"},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Source != "driver" {
		t.Fatalf("source = %q, want driver", result.Source)
	}
	if result.KeyFacts["project_key"] != "alpha" {
		t.Fatalf("project_key fact = %q, want alpha", result.KeyFacts["project_key"])
	}
	if result.KeyFacts["open_task_count"] != "1" {
		t.Fatalf("open_task_count = %q, want 1", result.KeyFacts["open_task_count"])
	}
	if !strings.Contains(result.RawOutput, "project=alpha") {
		t.Fatalf("raw output = %q, want project marker", result.RawOutput)
	}
}

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

func TestServiceRobinhoodTransferPreservesStructuredArtifacts(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood continuity check failed","artifacts":{"session_state":"resume_verification_failed","prior_session_state":"session_expired","evidence":["driver invoked"]}}'
`)
	t.Setenv("ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER", script)

	service := Service{}
	result, err := service.RobinhoodTransfer(context.Background(), webdriver.RobinhoodTransferRequest{
		Input: webdriver.RobinhoodTransferInput{
			Mode:               "submit",
			Direction:          "deposit",
			AmountUSD:          "25.00",
			SourceAccount:      "checking",
			DestinationAccount: "brokerage",
			ResumeFacts: map[string]string{
				"expected_review_state": "review_ready",
			},
		},
	})
	if err != nil {
		t.Fatalf("RobinhoodTransfer() error = %v", err)
	}
	if result.ToolKey != "robinhood_transfer_flow" {
		t.Fatalf("ToolKey = %q, want robinhood_transfer_flow", result.ToolKey)
	}
	if result.RawOutput != `{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood continuity check failed","artifacts":{"session_state":"resume_verification_failed","prior_session_state":"session_expired","evidence":["driver invoked"]}}` {
		t.Fatalf("RawOutput = %q, want exact driver stdout", result.RawOutput)
	}
	if got := result.Artifacts["session_state"]; got != "resume_verification_failed" {
		t.Fatalf("Artifacts.session_state = %#v, want resume_verification_failed", got)
	}
	if got := result.Artifacts["prior_session_state"]; got != "session_expired" {
		t.Fatalf("Artifacts.prior_session_state = %#v, want session_expired", got)
	}
}

func TestCloneArtifactsDeepCopiesNestedValues(t *testing.T) {
	source := map[string]any{
		"session_state": "ready",
		"snapshots": []any{
			map[string]any{"name": "home", "url": "https://example.com"},
		},
	}

	cloned := cloneArtifacts(source)
	cloned["session_state"] = "mutated"
	clonedSnapshots := cloned["snapshots"].([]any)
	clonedSnapshot := clonedSnapshots[0].(map[string]any)
	clonedSnapshot["name"] = "changed"

	if got := source["session_state"]; got != "ready" {
		t.Fatalf("source session_state = %#v, want ready", got)
	}
	sourceSnapshots := source["snapshots"].([]any)
	sourceSnapshot := sourceSnapshots[0].(map[string]any)
	if got := sourceSnapshot["name"]; got != "home" {
		t.Fatalf("source snapshots[0].name = %#v, want home", got)
	}
}

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

	service := Service{ApprovedExternalMutation: true}
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

func TestServiceBlocksUnapprovedHuginnXPostPublish(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "huginn-x-post-publish-request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_x_post_publish","summary":"Published unapproved X post."}'
`)
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	service := Service{}
	_, err := service.HuginnXPostPublish(context.Background(), webdriver.XPublishRequest{
		ToolKey: "browser_x_post_publish",
		Input: webdriver.XPublishInput{
			PostText: "Unapproved X post must not publish.",
			Label:    "direct-invocation",
		},
	})
	if err == nil {
		t.Fatal("HuginnXPostPublish() error = nil, want approval-required refusal")
	}
	coded, ok := err.(interface{ Code() string })
	if !ok {
		t.Fatalf("HuginnXPostPublish() error = %T, want coded approval error", err)
	}
	if coded.Code() != "approval_required" {
		t.Fatalf("HuginnXPostPublish() code = %q, want approval_required", coded.Code())
	}
	if _, statErr := os.Stat(requestPath); !os.IsNotExist(statErr) {
		t.Fatalf("unapproved publish driver was invoked; Stat(requestPath) error = %v", statErr)
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

	service := Service{ApprovedExternalMutation: true}
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
	approvedMutationService := Service{ApprovedExternalMutation: true}
	if _, err := approvedMutationService.HuginnXPostPublish(context.Background(), webdriver.XPublishRequest{ToolKey: "browser_x_post_publish"}); err == nil {
		t.Fatal("HuginnXPostPublish() error = nil, want missing driver config failure")
	}
}

func openInvocationStore(t *testing.T, runtimeRoot string) *sqlite.Store {
	t.Helper()

	dataDir := filepath.Join(runtimeRoot, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(data) error = %v", err)
	}
	store, err := sqlite.Open(filepath.Join(dataDir, "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
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
