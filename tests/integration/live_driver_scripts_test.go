package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type driverScriptResponse struct {
	Status    string         `json:"status"`
	ToolKey   string         `json:"tool_key"`
	Summary   string         `json:"summary"`
	Artifacts map[string]any `json:"artifacts"`
}

func TestGoogleCalendarDriverUsesRepoLocalLibraryByDefault(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "google-calendar-off-dates.sh")
	callPath := filepath.Join(t.TempDir(), "google-call.txt")

	request := `{"tool_key":"google_calendar_off_dates","input":{"bid_period":"2026-05","calendar_id":"family@group.calendar.google.com","timezone":"America/Chicago"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_TEST_GOOGLE_CALL_PATH": callPath,
		"ODIN_TEST_GOOGLE_RESPONSE": `{"items":[
            {"start":{"date":"2026-05-03"},"end":{"date":"2026-05-05"}},
            {"start":{"dateTime":"2026-04-30T23:00:00-05:00"},"end":{"dateTime":"2026-05-01T01:00:00-05:00"}},
            {"start":{"dateTime":"2026-05-10T23:30:00-05:00"},"end":{"dateTime":"2026-05-11T01:00:00-05:00"}},
            {"start":{"date":"2026-06-01"},"end":{"date":"2026-06-02"}}
        ]}`,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.ToolKey != "google_calendar_off_dates" {
		t.Fatalf("ToolKey = %q, want google_calendar_off_dates", response.ToolKey)
	}

	gotDates := artifactStrings(response.Artifacts["off_dates"])
	wantDates := []string{"2026-05-01", "2026-05-03", "2026-05-04", "2026-05-10", "2026-05-11"}
	if !reflect.DeepEqual(gotDates, wantDates) {
		t.Fatalf("off_dates = %#v, want %#v", gotDates, wantDates)
	}

	recordedCall, err := os.ReadFile(callPath)
	if err != nil {
		t.Fatalf("ReadFile(callPath) error = %v", err)
	}
	recorded := string(recordedCall)
	if !strings.Contains(recorded, "family%40group.calendar.google.com") {
		t.Fatalf("calendar call = %q, want encoded calendar id", recorded)
	}
	if !strings.Contains(recorded, "timeMin=") || !strings.Contains(recorded, "timeMax=") {
		t.Fatalf("calendar call = %q, want bounded time range", recorded)
	}
}

func TestGoogleCalendarDriverHonorsExplicitLibraryOverride(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "google-calendar-off-dates.sh")
	fixtureDir := t.TempDir()
	googleLib := filepath.Join(fixtureDir, "google.sh")
	callPath := filepath.Join(fixtureDir, "google-call.txt")

	if err := os.WriteFile(googleLib, []byte(`#!/usr/bin/env bash
google_api_call() {
    printf '%s\t%s\n' "$1" "$2" >"$ODIN_TEST_GOOGLE_CALL_PATH"
    printf '%s' "$ODIN_TEST_GOOGLE_RESPONSE"
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(googleLib) error = %v", err)
	}

	request := `{"tool_key":"google_calendar_off_dates","input":{"bid_period":"2026-05","calendar_id":"primary","timezone":"America/Chicago"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_GOOGLE_LIB_PATH":       googleLib,
		"ODIN_TEST_GOOGLE_CALL_PATH": callPath,
		"ODIN_TEST_GOOGLE_RESPONSE":  `{"items":[]}`,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	recordedCall, err := os.ReadFile(callPath)
	if err != nil {
		t.Fatalf("ReadFile(callPath) error = %v", err)
	}
	if !strings.Contains(string(recordedCall), "/calendars/primary/events") {
		t.Fatalf("calendar call = %q, want primary calendar request", string(recordedCall))
	}
}

func TestGoogleCalendarDriverRefreshesTokenWithRepoLocalLibrary(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "google-calendar-off-dates.sh")
	fixtureDir := t.TempDir()
	curlPath := filepath.Join(fixtureDir, "curl")
	tracePath := filepath.Join(fixtureDir, "curl-trace.txt")
	cachePath := filepath.Join(fixtureDir, "token-cache.json")

	if err := os.WriteFile(curlPath, []byte(`#!/usr/bin/env bash
set -euo pipefail
url="${@: -1}"
printf '%s\n' "$url" >>"$ODIN_TEST_CURL_TRACE"
case "$url" in
  "https://oauth2.googleapis.com/token")
    printf '{"access_token":"token-123","expires_in":3600}\n200'
    ;;
  *"/calendars/primary/events"*)
    printf '{"items":[{"start":{"date":"2026-05-12"},"end":{"date":"2026-05-13"}}]}\n200'
    ;;
  *)
    exit 1
    ;;
esac
`), 0o755); err != nil {
		t.Fatalf("WriteFile(fake curl) error = %v", err)
	}

	request := `{"tool_key":"google_calendar_off_dates","input":{"bid_period":"2026-05","calendar_id":"primary","timezone":"America/Chicago"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"PATH":                        fixtureDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GOOGLE_TOKEN_CACHE":          cachePath,
		"GOOGLE_OAUTH_CLIENT_ID":      "client",
		"GOOGLE_OAUTH_CLIENT_SECRET":  "secret",
		"GOOGLE_OAUTH_REFRESH_TOKEN":  "refresh",
		"ODIN_TEST_CURL_TRACE":        tracePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if !reflect.DeepEqual(artifactStrings(response.Artifacts["off_dates"]), []string{"2026-05-12"}) {
		t.Fatalf("off_dates = %#v, want %#v", artifactStrings(response.Artifacts["off_dates"]), []string{"2026-05-12"})
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(trace) error = %v", err)
	}
	trace := string(traceBytes)
	if !strings.Contains(trace, "https://oauth2.googleapis.com/token") || !strings.Contains(trace, "/calendars/primary/events") {
		t.Fatalf("trace = %q, want token refresh and calendar request", trace)
	}
}

func TestGoogleCalendarDriverReturnsJsonFailureForInvalidTimezone(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "google-calendar-off-dates.sh")

	request := `{"tool_key":"google_calendar_off_dates","input":{"bid_period":"2026-05","calendar_id":"primary","timezone":"Mars/Olympus"}}`
	response := runDriverScript(t, scriptPath, request, nil)

	if response.Status != "failed" {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
	if response.Artifacts["reason"] != "invalid_timezone" {
		t.Fatalf("reason = %#v, want invalid_timezone", response.Artifacts["reason"])
	}
}

func TestHuginnDriverUsesRepoLocalLibraryByDefault(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-pbs-session.sh")
	tracePath := filepath.Join(t.TempDir(), "huginn-trace.txt")

	request := `{"tool_key":"huginn_pbs_session","input":{"bid_period":"2026-05","workflow_key":"pbs_may_bid","timezone":"America/Chicago"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_TEST_HUGINN_TRACE":    tracePath,
		"ODIN_TEST_HUGINN_HEALTH":   `{"ok":true,"browser":true,"page":true,"url":"https://jia.flica.net/online/mainmenu.cgi"}`,
		"ODIN_TEST_HUGINN_SNAPSHOT": "Main Menu\nBid Period May 2026",
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Artifacts["session_state"] != "ready" {
		t.Fatalf("session_state = %#v, want ready", response.Artifacts["session_state"])
	}
	if response.Artifacts["session_id"] != "session://default/flica/default" {
		t.Fatalf("session_id = %#v, want repo-local session handle", response.Artifacts["session_id"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(trace) error = %v", err)
	}
	trace := string(traceBytes)
	if !strings.Contains(trace, "start\n") || !strings.Contains(trace, "load:flica\n") || !strings.Contains(trace, "navigate:https://jia.flica.net/online/mainmenu.cgi\n") || !strings.Contains(trace, "stop\n") {
		t.Fatalf("trace = %q, want start/load/navigate/stop sequence", trace)
	}
}

func TestHuginnDriverUsesSnapshotTextForLoginDetection(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-pbs-session.sh")
	fixtureDir := t.TempDir()
	curlPath := filepath.Join(fixtureDir, "curl")
	sessionDir := filepath.Join(fixtureDir, "sessions")

	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(sessionDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "flica.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatalf("WriteFile(session file) error = %v", err)
	}
	if err := os.WriteFile(curlPath, []byte(`#!/usr/bin/env bash
set -euo pipefail
url="${@: -1}"
case "$url" in
  *"/health")
    printf '{"ok":true,"browser":true,"page":true,"url":"https://jia.flica.net/online/mainmenu.cgi"}'
    ;;
  *"/cookies/set")
    printf '{}'
    ;;
  *"/navigate")
    printf '{"url":"https://jia.flica.net/online/mainmenu.cgi"}'
    ;;
  *"/snapshot?compact=true")
    printf '%s' '{"snapshot":"Sign in\\nPassword"}'
    ;;
  *"/stop")
    printf '{}'
    ;;
  *)
    exit 1
    ;;
esac
`), 0o755); err != nil {
		t.Fatalf("WriteFile(fake curl) error = %v", err)
	}

	request := `{"tool_key":"huginn_pbs_session","input":{"bid_period":"2026-05","workflow_key":"pbs_may_bid","timezone":"America/Chicago"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"PATH":                     fixtureDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"ODIN_BROWSER_SESSION_DIR": sessionDir,
	})

	if response.Status != "failed" {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
	if response.Artifacts["session_state"] != "login_required" {
		t.Fatalf("session_state = %#v, want login_required", response.Artifacts["session_state"])
	}
}

func TestHuginnPBSSessionDriverScriptReportsLoginRequired(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-pbs-session.sh")

	request := `{"tool_key":"huginn_pbs_session","input":{"bid_period":"2026-05","workflow_key":"pbs_may_bid","timezone":"America/Chicago"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_TEST_HUGINN_HEALTH":   `{"ok":true,"browser":true,"page":true,"url":"https://pfloginapp.cloud.aa.com/loginb2e"}`,
		"ODIN_TEST_HUGINN_SNAPSHOT": "Sign in\nPassword",
	})

	if response.Status != "failed" {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
	if response.Artifacts["session_state"] != "login_required" {
		t.Fatalf("session_state = %#v, want login_required", response.Artifacts["session_state"])
	}
}

func runDriverScript(t *testing.T, scriptPath string, stdin string, extraEnv map[string]string) driverScriptResponse {
	t.Helper()

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = filepath.Dir(filepath.Dir(scriptPath))
	cmd.Stdin = bytes.NewBufferString(stdin)

	env := append([]string{}, os.Environ()...)
	for key, value := range extraEnv {
		env = append(env, key+"="+value)
	}
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s error = %v\n%s", scriptPath, err, string(output))
	}

	var response driverScriptResponse
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("%s output json error = %v\n%s", scriptPath, err, string(output))
	}
	return response
}

func artifactStrings(raw any) []string {
	switch value := raw.(type) {
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, item.(string))
		}
		return out
	case []string:
		return append([]string(nil), value...)
	default:
		return nil
	}
}
