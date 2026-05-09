package integration_test

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type driverScriptResponse struct {
	Status    string         `json:"status"`
	ToolKey   string         `json:"tool_key"`
	Summary   string         `json:"summary"`
	Artifacts map[string]any `json:"artifacts"`
}

type codexDriverResponse struct {
	Status   string            `json:"status"`
	Output   string            `json:"output"`
	Metadata map[string]string `json:"metadata"`
}

func writeJSON(t *testing.T, w http.ResponseWriter, body map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("Encode(response) error = %v", err)
	}
}

func TestGoogleCalendarDriverScriptNormalizesMonthOffDates(t *testing.T) {
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

	request := `{"tool_key":"google_calendar_off_dates","input":{"bid_period":"2026-05","calendar_id":"family@group.calendar.google.com","timezone":"America/Chicago"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_GOOGLE_LIB_PATH":       googleLib,
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

func TestHuginnPBSSessionDriverScriptValidatesReadySession(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-pbs-session.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "huginn-trace.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_server_start() { printf 'start\n' >>"$ODIN_TEST_HUGINN_TRACE"; }
browser_load_session() { printf 'load:%s\n' "$1" >>"$ODIN_TEST_HUGINN_TRACE"; }
browser_navigate() { printf 'navigate:%s\n' "$1" >>"$ODIN_TEST_HUGINN_TRACE"; }
browser_snapshot() { printf '%s' "$ODIN_TEST_HUGINN_SNAPSHOT"; }
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_HUGINN_TRACE"; }
_bc_curl() {
    if [[ "$1" == *"/health" ]]; then
        printf '%s' "$ODIN_TEST_HUGINN_HEALTH"
        return 0
    fi
    return 1
}
_ba_session_handle() { printf 'session://pbs/flica/default'; }
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_pbs_session","input":{"bid_period":"2026-05","workflow_key":"pbs_may_bid","timezone":"America/Chicago"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_HUGINN_TRACE":       tracePath,
		"ODIN_TEST_HUGINN_HEALTH":      `{"ok":true,"browser":true,"page":true,"url":"https://jia.flica.net/online/mainmenu.cgi"}`,
		"ODIN_TEST_HUGINN_SNAPSHOT":    "Main Menu\nBid Period May 2026",
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Artifacts["session_state"] != "ready" {
		t.Fatalf("session_state = %#v, want ready", response.Artifacts["session_state"])
	}
	if response.Artifacts["session_id"] != "session://pbs/flica/default" {
		t.Fatalf("session_id = %#v, want session handle", response.Artifacts["session_id"])
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

func TestHuginnPBSSessionDriverScriptReportsLoginRequired(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-pbs-session.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_server_start() { :; }
browser_load_session() { :; }
browser_navigate() { :; }
browser_snapshot() { printf '%s' "$ODIN_TEST_HUGINN_SNAPSHOT"; }
browser_server_stop() { :; }
_bc_curl() {
    if [[ "$1" == *"/health" ]]; then
        printf '%s' "$ODIN_TEST_HUGINN_HEALTH"
        return 0
    fi
    return 1
}
_ba_session_handle() { printf 'session://pbs/flica/default'; }
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_pbs_session","input":{"bid_period":"2026-05","workflow_key":"pbs_may_bid","timezone":"America/Chicago"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_HUGINN_HEALTH":      `{"ok":true,"browser":true,"page":true,"url":"https://pfloginapp.cloud.aa.com/loginb2e"}`,
		"ODIN_TEST_HUGINN_SNAPSHOT":    "Sign in\nPassword",
	})

	if response.Status != "failed" {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
	if response.Artifacts["session_state"] != "login_required" {
		t.Fatalf("session_state = %#v, want login_required", response.Artifacts["session_state"])
	}
}

func TestHuginnVisualAuditDriverScriptPrefersTrustedSessionWhenHeaded(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-visual-audit.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "visual-trace.txt")
	screenshotPath := filepath.Join(fixtureDir, "x-compose.png")
	statePath := filepath.Join(fixtureDir, "visual-state.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_VISUAL_TRACE"; printf 'ready\n' >"$ODIN_TEST_VISUAL_STATE"; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_VISUAL_TRACE"; printf 'raw-start\n' >"$ODIN_TEST_VISUAL_STATE"; }
browser_server_health() {
    if [[ -f "$ODIN_TEST_VISUAL_STATE" ]] && grep -q '^ready$' "$ODIN_TEST_VISUAL_STATE"; then
        printf '%s' '{"ok":true,"browser":true,"page":true,"url":"https://x.com/compose/post","title":"X"}'
    else
        printf '%s' '{"ok":true,"browser":false,"page":false,"url":"","title":""}'
    fi
}
browser_snapshot() {
    if [[ -f "$ODIN_TEST_VISUAL_STATE" ]] && grep -q '^ready$' "$ODIN_TEST_VISUAL_STATE"; then
        printf '%s' 'Compose post\nWhat is happening?!'
    fi
}
browser_bc_screenshot() {
    if [[ -f "$ODIN_TEST_VISUAL_STATE" ]] && grep -q '^ready$' "$ODIN_TEST_VISUAL_STATE"; then
        printf '%s' "$ODIN_TEST_VISUAL_SCREENSHOT"
        return 0
    fi
    return 1
}
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_VISUAL_TRACE"; }
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_visual_audit","input":{"target_url":"https://x.com/compose/post","label":"x-compose-preflight","headless":"false","screenshot_path":"` + screenshotPath + `"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_VISUAL_TRACE":       tracePath,
		"ODIN_TEST_VISUAL_STATE":       statePath,
		"ODIN_TEST_VISUAL_SCREENSHOT":  screenshotPath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Artifacts["final_url"] != "https://x.com/compose/post" {
		t.Fatalf("final_url = %#v, want x compose url", response.Artifacts["final_url"])
	}
	if response.Artifacts["screenshot_path"] != screenshotPath {
		t.Fatalf("screenshot_path = %#v, want fixture screenshot path", response.Artifacts["screenshot_path"])
	}
	if response.Artifacts["launch_mode"] != "--headed" {
		t.Fatalf("launch_mode = %#v, want --headed", response.Artifacts["launch_mode"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	if !strings.Contains(trace, "trusted:--url https://x.com/compose/post\n") {
		t.Fatalf("trace = %q, want trusted headed session start", trace)
	}
	if strings.Contains(trace, "start:") {
		t.Fatalf("trace = %q, want no raw browser_server_start fallback", trace)
	}
}

func TestHuginnVisualAuditDriverScriptTreatsAttachedTrustedSessionPageAsReady(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-visual-audit.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "visual-trace.txt")
	screenshotPath := filepath.Join(fixtureDir, "x-compose.png")
	statePath := filepath.Join(fixtureDir, "visual-state.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_VISUAL_TRACE"; printf 'ready\n' >"$ODIN_TEST_VISUAL_STATE"; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_VISUAL_TRACE"; printf 'raw-start\n' >"$ODIN_TEST_VISUAL_STATE"; }
browser_server_health() {
    if [[ -f "$ODIN_TEST_VISUAL_STATE" ]] && grep -q '^ready$' "$ODIN_TEST_VISUAL_STATE"; then
        printf '%s' '{"ok":true,"browser":false,"page":true,"url":"https://x.com/compose/post","title":"X"}'
    else
        printf '%s' '{"ok":true,"browser":false,"page":false,"url":"","title":""}'
    fi
}
browser_snapshot() {
    if [[ -f "$ODIN_TEST_VISUAL_STATE" ]] && grep -q '^ready$' "$ODIN_TEST_VISUAL_STATE"; then
        printf '%s' 'Compose post\nWhat is happening?!'
    fi
}
browser_bc_screenshot() {
    if [[ -f "$ODIN_TEST_VISUAL_STATE" ]] && grep -q '^ready$' "$ODIN_TEST_VISUAL_STATE"; then
        printf '%s' "$ODIN_TEST_VISUAL_SCREENSHOT"
        return 0
    fi
    return 1
}
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_VISUAL_TRACE"; }
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_visual_audit","input":{"target_url":"https://x.com/compose/post","label":"x-compose-preflight","headless":"false","screenshot_path":"` + screenshotPath + `"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_VISUAL_TRACE":       tracePath,
		"ODIN_TEST_VISUAL_STATE":       statePath,
		"ODIN_TEST_VISUAL_SCREENSHOT":  screenshotPath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed for attached trusted session page", response.Status)
	}
	if response.Artifacts["final_url"] != "https://x.com/compose/post" {
		t.Fatalf("final_url = %#v, want x compose url", response.Artifacts["final_url"])
	}
	if response.Artifacts["launch_mode"] != "--headed" {
		t.Fatalf("launch_mode = %#v, want --headed", response.Artifacts["launch_mode"])
	}
}

func TestPlaidSupportCaseDriverScriptMountsPricingForm(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "plaid-support-case.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "plaid-support-trace.txt")
	statePath := filepath.Join(fixtureDir, "plaid-support-state.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_type_selector() {
    local selector="${1:-}" text="${2:-}" submit="false"
    shift 2 || true
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --submit)
                submit="true"
                ;;
        esac
        shift
    done
    printf 'type:%s:%s:%s\n' "$selector" "$text" "$submit" >>"$ODIN_TEST_SUPPORT_TRACE"
    if [[ "$selector" == "#secondaryCategoryDropdown-input" ]]; then
        printf 'pricing\n' >"$ODIN_TEST_SUPPORT_STATE"
    fi
}
browser_snapshot() {
    if [[ -f "$ODIN_TEST_SUPPORT_STATE" ]]; then
        printf '%s' 'Plaid - Dashboard\n\nCase Details\nSubject\nSelect country\nDescription'
    else
        printf '%s' 'Plaid - Dashboard\n\nQuestion Type'
    fi
}
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_SUPPORT_TRACE"
    case "$fn" in
        *"Continue to open a case"*)
            printf '%s' '{"ok":true}'
            ;;
        *"location.href"*)
            printf '%s' '{"url":"https://dashboard.plaid.com/support/new/admin/account-administration/pricing","title":"Plaid - Dashboard","hasSubject":true,"hasCountry":true,"hasBody":true,"buttons":["Contact Plaid Support"],"text":"Case Details\nSubject\nSelect country\nDescription"}'
            ;;
        *'!!document.querySelector("#subject")'*)
            if [[ -f "$ODIN_TEST_SUPPORT_STATE" ]]; then
                printf 'true'
            else
                printf 'false'
            fi
            ;;
        *"Contact Plaid Support"*)
            printf '%s' '{"ok":true}'
            ;;
        *)
            printf 'null'
            ;;
    esac
}
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_SUPPORT_TRACE"; }
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"plaid_support_case","input":{"support_url":"https://dashboard.plaid.com/support/new/admin/account-administration","category":"Plaid pricing and billing","subject":"Request Plaid Transfer product access / upgrade","country":"United States","body":"Please enable Transfer for this account.","submit":false}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_SUPPORT_TRACE":      tracePath,
		"ODIN_TEST_SUPPORT_STATE":      statePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.ToolKey != "plaid_support_case" {
		t.Fatalf("ToolKey = %q, want plaid_support_case", response.ToolKey)
	}
	if response.Artifacts["session_state"] != "pricing_case_form_ready" {
		t.Fatalf("session_state = %#v, want pricing_case_form_ready", response.Artifacts["session_state"])
	}
	if response.Artifacts["current_url"] != "https://dashboard.plaid.com/support/new/admin/account-administration/pricing" {
		t.Fatalf("current_url = %#v, want pricing support url", response.Artifacts["current_url"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	for _, want := range []string{
		"start:--url https://dashboard.plaid.com/support/new/admin/account-administration\n",
		"click:#secondaryCategoryDropdown .css-1012nnc-control\n",
		"type:#secondaryCategoryDropdown-input:Plaid pricing and billing:true\n",
		"type:#subject:Request Plaid Transfer product access / upgrade:false\n",
		"type:#countryCode-input:United States:true\n",
		"type:#body:Please enable Transfer for this account.:false\n",
		"stop\n",
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace = %q, want %q", trace, want)
		}
	}
}

func TestPlaidSupportCaseDriverScriptUsesExistingPricingFormWithoutContinueStep(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "plaid-support-case.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "plaid-support-direct-trace.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_type_selector() {
    local selector="${1:-}" text="${2:-}" submit="false"
    shift 2 || true
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --submit)
                submit="true"
                ;;
        esac
        shift
    done
    printf 'type:%s:%s:%s\n' "$selector" "$text" "$submit" >>"$ODIN_TEST_SUPPORT_TRACE"
}
browser_snapshot() { printf '%s' 'Plaid - Dashboard\n\nCase Details\nSubject\nSelect country\nDescription'; }
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_SUPPORT_TRACE"
    case "$fn" in
        *"location.href"*)
            printf '%s' '{"url":"https://dashboard.plaid.com/support/new/admin/account-administration/pricing","title":"Plaid - Dashboard","hasSubject":true,"hasCountry":true,"hasBody":true,"buttons":["Contact Plaid Support"],"text":"Case Details\nSubject\nSelect country\nDescription"}'
            ;;
        *'!!document.querySelector("#subject")'*)
            printf 'true'
            ;;
        *"Contact Plaid Support"*)
            printf '%s' '{"ok":true}'
            ;;
        *)
            printf 'null'
            ;;
    esac
}
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_SUPPORT_TRACE"; }
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"plaid_support_case","input":{"support_url":"https://dashboard.plaid.com/support/new/admin/account-administration/pricing","category":"Plaid pricing and billing","subject":"Request Plaid Transfer product access / upgrade","country":"United States","body":"Please enable Transfer for this account.","submit":false}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_SUPPORT_TRACE":      tracePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Artifacts["session_state"] != "pricing_case_form_ready" {
		t.Fatalf("session_state = %#v, want pricing_case_form_ready", response.Artifacts["session_state"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	for _, want := range []string{
		"start:--url https://dashboard.plaid.com/support/new/admin/account-administration/pricing\n",
		"type:#subject:Request Plaid Transfer product access / upgrade:false\n",
		"type:#countryCode-input:United States:true\n",
		"type:#body:Please enable Transfer for this account.:false\n",
		"stop\n",
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace = %q, want %q", trace, want)
		}
	}
	for _, unwanted := range []string{
		"click:#secondaryCategoryDropdown .css-1012nnc-control\n",
		"type:#secondaryCategoryDropdown-input:Plaid pricing and billing:true\n",
	} {
		if strings.Contains(trace, unwanted) {
			t.Fatalf("trace = %q, want pricing form path to skip %q", trace, unwanted)
		}
	}
}

func TestPlaidSupportCaseDriverScriptWaitsPastJavascriptShell(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "plaid-support-case.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "plaid-support-shell-trace.txt")
	countPath := filepath.Join(fixtureDir, "snapshot-count.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_type_selector() {
    local selector="${1:-}" text="${2:-}" submit="false"
    shift 2 || true
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --submit)
                submit="true"
                ;;
        esac
        shift
    done
    printf 'type:%s:%s:%s\n' "$selector" "$text" "$submit" >>"$ODIN_TEST_SUPPORT_TRACE"
}
browser_snapshot() {
    local count=0
    if [[ -f "$ODIN_TEST_SUPPORT_COUNT" ]]; then
        count="$(cat "$ODIN_TEST_SUPPORT_COUNT")"
    fi
    count=$((count + 1))
    printf '%s' "$count" >"$ODIN_TEST_SUPPORT_COUNT"
    if [[ "$count" -lt 3 ]]; then
        printf '%s' 'Plaid - Dashboard\n\n    You need to enable JavaScript to run this app.'
    else
        printf '%s' 'Plaid - Dashboard\n\nCase Details\nSubject\nSelect country\nDescription'
    fi
}
browser_evaluate() {
    local fn="${1:-}" count=0
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_SUPPORT_TRACE"
    if [[ -f "$ODIN_TEST_SUPPORT_COUNT" ]]; then
        count="$(cat "$ODIN_TEST_SUPPORT_COUNT")"
    fi
    case "$fn" in
        *"location.href"*)
            if [[ "$count" -lt 3 ]]; then
                printf '%s' '{"url":"https://dashboard.plaid.com/support/new/admin/account-administration/pricing","title":"Plaid - Dashboard","hasSubject":false,"hasCountry":false,"hasBody":false,"buttons":[],"text":"You need to enable JavaScript to run this app."}'
            else
                printf '%s' '{"url":"https://dashboard.plaid.com/support/new/admin/account-administration/pricing","title":"Plaid - Dashboard","hasSubject":true,"hasCountry":true,"hasBody":true,"buttons":["Contact Plaid Support"],"text":"Case Details\nSubject\nSelect country\nDescription"}'
            fi
            ;;
        *'!!document.querySelector("#subject")'*)
            if [[ "$count" -lt 3 ]]; then
                printf 'false'
            else
                printf 'true'
            fi
            ;;
        *"Contact Plaid Support"*)
            printf '%s' '{"ok":true}'
            ;;
        *)
            printf 'null'
            ;;
    esac
}
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_SUPPORT_TRACE"; }
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"plaid_support_case","input":{"support_url":"https://dashboard.plaid.com/support/new/admin/account-administration/pricing","category":"Plaid pricing and billing","subject":"Request Plaid Transfer product access / upgrade","country":"United States","body":"Please enable Transfer for this account.","submit":false}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_SUPPORT_TRACE":      tracePath,
		"ODIN_TEST_SUPPORT_COUNT":      countPath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Artifacts["session_state"] != "pricing_case_form_ready" {
		t.Fatalf("session_state = %#v, want pricing_case_form_ready", response.Artifacts["session_state"])
	}
}

func TestPlaidSupportCaseDriverScriptUsesInvokedEvaluateExpressions(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "plaid-support-case.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "plaid-support-eval-trace.txt")
	statePath := filepath.Join(fixtureDir, "plaid-support-eval-state.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_navigate() { printf 'navigate:%s\n' "$1" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_type_selector() {
    local selector="${1:-}" text="${2:-}" submit="false"
    shift 2 || true
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --submit)
                submit="true"
                ;;
        esac
        shift
    done
    printf 'type:%s:%s:%s\n' "$selector" "$text" "$submit" >>"$ODIN_TEST_SUPPORT_TRACE"
}
browser_snapshot() {
    if [[ -f "$ODIN_TEST_SUPPORT_STATE" ]]; then
        printf '%s' 'Plaid - Dashboard\n\nCase Details\nSubject\nSelect country\nDescription'
    else
        printf '%s' 'Plaid - Dashboard\n\nSupport\nContinue to open a case'
    fi
}
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_SUPPORT_TRACE"
    case "$fn" in
        '(() => {'*'})()'|*'(() => ({'*'}))()'*|*'(() => !!document.querySelector("#subject"))()'*)
            ;;
        *)
            printf 'null'
            return 0
            ;;
    esac
    case "$fn" in
        *"Continue to open a case"*)
            printf 'pricing\n' >"$ODIN_TEST_SUPPORT_STATE"
            printf '%s' '{"ok":true}'
            ;;
        *"location.href"*)
            if [[ -f "$ODIN_TEST_SUPPORT_STATE" ]]; then
                printf '%s' '{"url":"https://dashboard.plaid.com/support/new/admin/account-administration/pricing","title":"Plaid - Dashboard","hasSubject":true,"hasCountry":true,"hasBody":true,"buttons":["Contact Plaid Support"],"text":"Case Details\nSubject\nSelect country\nDescription"}'
            else
                printf '%s' '{"url":"https://dashboard.plaid.com/support/new/admin/account-administration","title":"Plaid - Dashboard","hasSubject":false,"hasCountry":false,"hasBody":false,"buttons":["Continue to open a case"],"text":"Support\nContinue to open a case"}'
            fi
            ;;
        *'!!document.querySelector("#subject")'*)
            if [[ -f "$ODIN_TEST_SUPPORT_STATE" ]]; then
                printf 'true'
            else
                printf 'false'
            fi
            ;;
        *"Contact Plaid Support"*)
            printf '%s' '{"ok":true}'
            ;;
        *)
            printf 'null'
            ;;
    esac
}
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_SUPPORT_TRACE"; }
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"plaid_support_case","input":{"support_url":"https://dashboard.plaid.com/support/new/admin/account-administration","category":"Plaid pricing and billing","subject":"Request Plaid Transfer product access / upgrade","country":"United States","body":"Please enable Transfer for this account.","submit":false}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_SUPPORT_TRACE":      tracePath,
		"ODIN_TEST_SUPPORT_STATE":      statePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Artifacts["session_state"] != "pricing_case_form_ready" {
		t.Fatalf("session_state = %#v, want pricing_case_form_ready", response.Artifacts["session_state"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	for _, want := range []string{
		"eval:(() => ({ url: location.href, title: document.title, hasSubject: !!document.querySelector(\"#subject\"), hasCountry: !!document.querySelector(\"#countryCode-input\"), hasBody: !!document.querySelector(\"#body\"), buttons: [...document.querySelectorAll(\"button\")].map(el => (el.textContent || \"\").trim()).filter(Boolean).slice(0, 20), text: document.body.innerText.slice(0, 2000) }))()\n",
		"eval:(() => !!document.querySelector(\"#subject\"))()\n",
		"eval:(() => { const btn = [...document.querySelectorAll(\"button\")].find(el => /Continue to open a case/i.test(el.textContent || \"\")); if (!btn) return { ok: false, reason: \"continue_not_found\" }; btn.click(); return { ok: true }; })()\n",
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace = %q, want %q", trace, want)
		}
	}
}

func TestPlaidSupportCaseDriverScriptReportsTransferUpgradeRedirect(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "plaid-support-case.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "plaid-support-upgrade-trace.txt")
	statePath := filepath.Join(fixtureDir, "plaid-support-upgrade-state.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_navigate() { printf 'navigate:%s\n' "$1" >>"$ODIN_TEST_SUPPORT_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_SUPPORT_TRACE"; return 1; }
browser_type_selector() {
    local selector="${1:-}" text="${2:-}" submit="false"
    shift 2 || true
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --submit)
                submit="true"
                ;;
        esac
        shift
    done
    printf 'type:%s:%s:%s\n' "$selector" "$text" "$submit" >>"$ODIN_TEST_SUPPORT_TRACE"
    if [[ "$selector" == "#secondaryCategoryDropdown-input" ]]; then
        printf 'products\n' >"$ODIN_TEST_SUPPORT_STATE"
    fi
}
browser_snapshot() {
    if [[ -f "$ODIN_TEST_SUPPORT_STATE" ]]; then
        printf '%s' 'Plaid - Dashboard\n\nProducts\nAll products\nTransfer\nUpgrade plan'
    else
        printf '%s' 'Plaid - Dashboard\n\nSupport\nContinue to open a case'
    fi
}
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_SUPPORT_TRACE"
    case "$fn" in
        '(() => {'*'})()'|*'(() => ({'*'}))()'*|*'(() => !!document.querySelector("#subject"))()'*)
            ;;
        *)
            printf 'null'
            return 0
            ;;
    esac
    case "$fn" in
        *"Continue to open a case"*)
            printf '%s' '{"ok":true}'
            ;;
        *"location.href"*)
            if [[ -f "$ODIN_TEST_SUPPORT_STATE" ]]; then
                printf '%s' '{"url":"https://dashboard.plaid.com/settings/team/products","title":"Plaid - Dashboard","hasSubject":false,"hasCountry":false,"hasBody":false,"buttons":["Upgrade plan"],"text":"Products\nAll products\nTransfer\nUpgrade plan"}'
            else
                printf '%s' '{"url":"https://dashboard.plaid.com/support/new/admin/account-administration","title":"Plaid - Dashboard","hasSubject":false,"hasCountry":false,"hasBody":false,"buttons":["Continue to open a case"],"text":"Support\nContinue to open a case"}'
            fi
            ;;
        *'!!document.querySelector("#subject")'*)
            printf 'false'
            ;;
        *)
            printf 'null'
            ;;
    esac
}
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_SUPPORT_TRACE"; }
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"plaid_support_case","input":{"support_url":"https://dashboard.plaid.com/support/new/admin/account-administration","category":"Plaid pricing and billing","subject":"Request Plaid Transfer product access / upgrade","country":"United States","body":"Please enable Transfer for this account.","submit":true}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_SUPPORT_TRACE":      tracePath,
		"ODIN_TEST_SUPPORT_STATE":      statePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Artifacts["session_state"] != "transfer_upgrade_required" {
		t.Fatalf("session_state = %#v, want transfer_upgrade_required", response.Artifacts["session_state"])
	}
	if response.Artifacts["current_url"] != "https://dashboard.plaid.com/settings/team/products" {
		t.Fatalf("current_url = %#v, want products page", response.Artifacts["current_url"])
	}
	if !strings.Contains(response.Summary, "Upgrade plan") {
		t.Fatalf("Summary = %q, want upgrade-plan summary", response.Summary)
	}
}

func TestHuginnXPostPublishDriverScriptPublishesApprovedPost(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-x-post-publish.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "huginn-x-publish-trace.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_health() { printf '%s' '{"ok":true,"browser":true,"page":true,"url":"https://x.com/compose/post"}'; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_type_selector() { printf 'type:%s:%s\n' "$1" "$2" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_bc_screenshot() { printf 'screenshot:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_X_PUBLISH_TRACE"
    if [[ "$fn" == *"composer_selector"* ]]; then
        printf '%s' '{"composer_selector":"[data-testid=\"tweetTextarea_0\"]","button_selector":"[data-testid=\"tweetButton\"]","current_url":"https://x.com/compose/post","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"button_ready"* ]]; then
        printf '%s' '{"button_ready":true,"current_url":"https://x.com/compose/post","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"publish_url"* ]]; then
        printf '%s' '{"publish_url":"https://x.com/marcus/status/999999999","final_url":"https://x.com/marcus/status/999999999","title":"X"}'
        return 0
    fi
    printf '{}'
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_x_post_publish","input":{"post_text":"Approved X post ready to publish natively.","label":"social-outcome-2","wait_ms":"0"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_X_PUBLISH_TRACE":    tracePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.ToolKey != "browser_x_post_publish" {
		t.Fatalf("ToolKey = %q, want browser_x_post_publish", response.ToolKey)
	}
	if response.Artifacts["publish_url"] != "https://x.com/marcus/status/999999999" {
		t.Fatalf("publish_url = %#v, want expected URL", response.Artifacts["publish_url"])
	}
	if response.Artifacts["screenshot_path"] == "" {
		t.Fatalf("screenshot_path = %#v, want non-empty path", response.Artifacts["screenshot_path"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	for _, want := range []string{
		"trusted:--url https://x.com/compose/post\n",
		"type:[data-testid=\"tweetTextarea_0\"]:Approved X post ready to publish natively.\n",
		"click:[data-testid=\"tweetButton\"]\n",
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace = %q, want %q", trace, want)
		}
	}
}

func TestHuginnXPostPublishDriverScriptPrefersEnabledTweetButtonOverDisabledInline(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-x-post-publish.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "huginn-x-publish-trace.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_health() { printf '%s' '{"ok":true,"browser":true,"page":true,"url":"https://x.com/compose/post"}'; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_type_selector() { printf 'type:%s:%s\n' "$1" "$2" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_bc_screenshot() { printf 'screenshot:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_X_PUBLISH_TRACE"
    if [[ "$fn" == *"composer_selector"* ]]; then
        if [[ "$fn" == *"aria-disabled"* && "$fn" == *"candidate.disabled"* ]]; then
            printf '%s' '{"composer_selector":"[data-testid=\"tweetTextarea_0\"]","button_selector":"[data-testid=\"tweetButton\"]","current_url":"https://x.com/compose/post","title":"X"}'
            return 0
        fi
        printf '%s' '{"composer_selector":"[data-testid=\"tweetTextarea_0\"]","button_selector":"[data-testid=\"tweetButtonInline\"]","current_url":"https://x.com/compose/post","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"button_ready"* ]]; then
        printf '%s' '{"button_ready":true,"current_url":"https://x.com/compose/post","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"publish_url"* ]]; then
        printf '%s' '{"publish_url":"https://x.com/marcus/status/999999999","final_url":"https://x.com/marcus/status/999999999","title":"X"}'
        return 0
    fi
    printf '{}'
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_x_post_publish","input":{"post_text":"Approved X post ready to publish natively.","label":"social-outcome-2","wait_ms":"0"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_X_PUBLISH_TRACE":    tracePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	if !strings.Contains(trace, "click:[data-testid=\"tweetButton\"]\n") {
		t.Fatalf("trace = %q, want enabled tweetButton click", trace)
	}
	if strings.Contains(trace, "click:[data-testid=\"tweetButtonInline\"]\n") {
		t.Fatalf("trace = %q, want no disabled inline button click", trace)
	}
}

func TestHuginnXPostPublishDriverScriptWaitsForSelectedButtonToBecomeReady(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-x-post-publish.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "huginn-x-publish-trace.txt")
	statePath := filepath.Join(fixtureDir, "huginn-x-publish-state.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_health() { printf '%s' '{"ok":true,"browser":true,"page":true,"url":"https://x.com/compose/post"}'; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_type_selector() { printf 'type:%s:%s\n' "$1" "$2" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_click_selector() {
    if [[ ! -f "$ODIN_TEST_X_PUBLISH_STATE" ]] || ! grep -q '^ready$' "$ODIN_TEST_X_PUBLISH_STATE"; then
        return 1
    fi
    printf 'click:%s\n' "$1" >>"$ODIN_TEST_X_PUBLISH_TRACE"
}
browser_bc_screenshot() { printf 'screenshot:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_X_PUBLISH_TRACE"
    if [[ "$fn" == *"composer_selector"* ]]; then
        printf '%s' '{"composer_selector":"[data-testid=\"tweetTextarea_0\"]","button_selector":"[data-testid=\"tweetButton\"]","current_url":"https://x.com/compose/post","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"button_ready"* ]]; then
        if [[ ! -f "$ODIN_TEST_X_PUBLISH_STATE" ]]; then
            printf '%s' 'pending' >"$ODIN_TEST_X_PUBLISH_STATE"
            printf '%s' '{"button_ready":false,"current_url":"https://x.com/compose/post","title":"X"}'
            return 0
        fi
        printf '%s' 'ready' >"$ODIN_TEST_X_PUBLISH_STATE"
        printf '%s' '{"button_ready":true,"current_url":"https://x.com/compose/post","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"publish_url"* ]]; then
        printf '%s' '{"publish_url":"https://x.com/marcus/status/999999999","final_url":"https://x.com/marcus/status/999999999","title":"X"}'
        return 0
    fi
    printf '{}'
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_x_post_publish","input":{"post_text":"Approved X post ready to publish natively.","label":"social-outcome-2","wait_ms":"0"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_X_PUBLISH_TRACE":    tracePath,
		"ODIN_TEST_X_PUBLISH_STATE":    statePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	if strings.Count(trace, "button_ready") < 2 {
		t.Fatalf("trace = %q, want repeated button readiness checks", trace)
	}
	if !strings.Contains(trace, "click:[data-testid=\"tweetButton\"]\n") {
		t.Fatalf("trace = %q, want tweetButton click after readiness", trace)
	}
}

func TestHuginnXPostPublishDriverScriptPublishesApprovedReply(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-x-post-publish.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "huginn-x-reply-trace.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_health() { printf '%s' '{"ok":true,"browser":true,"page":true,"url":"https://x.com/example/status/123","title":"X"}'; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_type_selector() { printf 'type:%s:%s\n' "$1" "$2" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_bc_screenshot() { printf 'screenshot:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_X_PUBLISH_TRACE"
    if [[ "$fn" == *"composer_selector"* ]]; then
        printf '%s' '{"composer_selector":"[data-testid=\"tweetTextarea_0\"]","button_selector":"[data-testid=\"tweetButton\"]","current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"button_ready"* ]]; then
        printf '%s' '{"button_ready":true,"current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"publish_url"* ]]; then
        printf '%s' '{"publish_url":"https://x.com/marcus/status/888888888","final_url":"https://x.com/marcus/status/888888888","title":"X"}'
        return 0
    fi
    printf '{}'
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_x_post_publish","input":{"post_text":"Short, useful reply text.","content_kind":"reply","in_reply_to_url":"https://x.com/example/status/123","label":"social-outcome-9","wait_ms":"0"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_X_PUBLISH_TRACE":    tracePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Artifacts["publish_url"] != "https://x.com/marcus/status/888888888" {
		t.Fatalf("publish_url = %#v, want expected reply url", response.Artifacts["publish_url"])
	}
	if response.Artifacts["in_reply_to_url"] != "https://x.com/example/status/123" {
		t.Fatalf("in_reply_to_url = %#v, want reply target", response.Artifacts["in_reply_to_url"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	for _, want := range []string{
		"trusted:--url https://x.com/example/status/123\n",
		"type:[data-testid=\"tweetTextarea_0\"]:Short, useful reply text.\n",
		"click:[data-testid=\"tweetButton\"]\n",
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace = %q, want %q", trace, want)
		}
	}
}

func TestHuginnXPostPublishDriverScriptPublishesApprovedReplyFromReplyPageInlineButton(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-x-post-publish.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "huginn-x-reply-inline-trace.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_health() { printf '%s' '{"ok":true,"browser":false,"page":true,"url":"https://x.com/example/status/123","title":"X"}'; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_type_selector() { printf 'type:%s:%s\n' "$1" "$2" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_bc_screenshot() { printf 'screenshot:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_X_PUBLISH_TRACE"
    if [[ "$fn" == *"composer_selector"* ]]; then
        printf '%s' '{"composer_selector":"[data-testid=\"tweetTextarea_0\"]","button_selector":"[data-testid=\"tweetButtonInline\"]","current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"button_ready"* ]]; then
        printf '%s' '{"button_ready":true,"current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"clicked"* ]]; then
        printf '%s' '{"clicked":true,"current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"publish_url"* ]]; then
        printf '%s' '{"publish_url":"https://x.com/marcus/status/888888889","final_url":"https://x.com/marcus/status/888888889","title":"X"}'
        return 0
    fi
    printf '{}'
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_x_post_publish","input":{"post_text":"Short, useful reply text.","content_kind":"reply","in_reply_to_url":"https://x.com/example/status/123","label":"social-outcome-36","wait_ms":"0"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_X_PUBLISH_TRACE":    tracePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed (summary=%q)", response.Status, response.Summary)
	}
	if response.Artifacts["publish_url"] != "https://x.com/marcus/status/888888889" {
		t.Fatalf("publish_url = %#v, want expected reply url", response.Artifacts["publish_url"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	for _, want := range []string{
		"trusted:--url https://x.com/example/status/123\n",
		"type:[data-testid=\"tweetTextarea_0\"]:Short, useful reply text.\n",
		"document.querySelector(\"[data-testid=\\\"tweetButtonInline\\\"]\")",
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace = %q, want %q", trace, want)
		}
	}
}

func TestHuginnXPostPublishDriverScriptFailsReplyWithoutTargetURL(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-x-post-publish.sh")

	request := `{"tool_key":"browser_x_post_publish","input":{"post_text":"Short, useful reply text.","content_kind":"reply","label":"social-outcome-9","wait_ms":"0"}}`
	response := runDriverScript(t, scriptPath, request, nil)

	if response.Status != "failed" {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
	if !strings.Contains(response.Summary, "in_reply_to_url is required") {
		t.Fatalf("Summary = %q, want missing target validation", response.Summary)
	}
}

func TestHuginnXPostPublishDriverScriptRejectsReplyPublishURLThatMatchesOriginalTarget(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-x-post-publish.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "huginn-x-reply-verify-trace.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_health() { printf '%s' '{"ok":true,"browser":false,"page":true,"url":"https://x.com/example/status/123","title":"X"}'; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_type_selector() { printf 'type:%s:%s\n' "$1" "$2" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_bc_screenshot() { printf 'screenshot:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_X_PUBLISH_TRACE"
    if [[ "$fn" == *"composer_selector"* ]]; then
        printf '%s' '{"composer_selector":"[data-testid=\"tweetTextarea_0\"]","button_selector":"[data-testid=\"tweetButtonInline\"]","current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"button_ready"* ]]; then
        printf '%s' '{"button_ready":true,"current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"clicked"* ]]; then
        printf '%s' '{"clicked":true,"current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"publish_url"* ]]; then
        printf '%s' '{"publish_url":"https://x.com/example/status/123","final_url":"https://x.com/example/status/123/quick_promote_web/targeting","title":"X"}'
        return 0
    fi
    printf '{}'
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_x_post_publish","input":{"post_text":"Short, useful reply text.","content_kind":"reply","in_reply_to_url":"https://x.com/example/status/123","label":"social-outcome-36","wait_ms":"0"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_X_PUBLISH_TRACE":    tracePath,
	})

	if response.Status != "failed" {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
	if !strings.Contains(response.Summary, "Unable to verify the resulting X post URL after publish.") {
		t.Fatalf("Summary = %q, want reply publish URL verification failure", response.Summary)
	}
}

func TestHuginnXPostPublishDriverScriptPublishesApprovedReplyAfterComposeModalOpens(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-x-post-publish.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "huginn-x-reply-compose-modal-trace.txt")
	statePath := filepath.Join(fixtureDir, "publish-state.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
: >"$ODIN_TEST_X_PUBLISH_STATE"
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_health() { printf '%s' '{"ok":true,"browser":false,"page":true,"url":"https://x.com/example/status/123","title":"X"}'; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_type_selector() { printf 'type:%s:%s\n' "$1" "$2" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_bc_screenshot() { printf 'screenshot:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_X_PUBLISH_TRACE"
    if [[ "$fn" == *"composer_selector"* ]]; then
        if grep -q '^compose-opened$' "$ODIN_TEST_X_PUBLISH_STATE" 2>/dev/null; then
            printf '%s' '{"composer_selector":"[data-testid=\"tweetTextarea_0\"]","button_selector":"[data-testid=\"tweetButton\"]","current_url":"https://x.com/compose/post","title":"Post"}'
        else
            printf '%s' '{"composer_selector":"[data-testid=\"tweetTextarea_0\"]","button_selector":"[data-testid=\"tweetButtonInline\"]","current_url":"https://x.com/example/status/123","title":"X"}'
        fi
        return 0
    fi
    if [[ "$fn" == *"button_ready"* ]]; then
        printf '%s' '{"button_ready":true,"current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"clicked"* ]]; then
        if [[ "$fn" == *'tweetButtonInline'* ]]; then
            printf 'compose-opened\n' >"$ODIN_TEST_X_PUBLISH_STATE"
            printf '%s' '{"clicked":true,"current_url":"https://x.com/compose/post","title":"Post"}'
        else
            printf 'posted\n' >"$ODIN_TEST_X_PUBLISH_STATE"
            printf '%s' '{"clicked":true,"current_url":"https://x.com/marcus/status/888888890","title":"X"}'
        fi
        return 0
    fi
    if [[ "$fn" == *"publish_url"* ]]; then
        if grep -q '^posted$' "$ODIN_TEST_X_PUBLISH_STATE" 2>/dev/null; then
            printf '%s' '{"publish_url":"https://x.com/marcus/status/888888890","final_url":"https://x.com/marcus/status/888888890","title":"X"}'
        else
            printf '%s' '{"publish_url":"","final_url":"https://x.com/compose/post","title":"Post"}'
        fi
        return 0
    fi
    printf '{}'
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_x_post_publish","input":{"post_text":"Short, useful reply text.","content_kind":"reply","in_reply_to_url":"https://x.com/example/status/123","label":"social-outcome-36","wait_ms":"0"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_X_PUBLISH_TRACE":    tracePath,
		"ODIN_TEST_X_PUBLISH_STATE":    statePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed (summary=%q)", response.Status, response.Summary)
	}
	if response.Artifacts["publish_url"] != "https://x.com/marcus/status/888888890" {
		t.Fatalf("publish_url = %#v, want expected reply url", response.Artifacts["publish_url"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	for _, want := range []string{
		"document.querySelector(\"[data-testid=\\\"tweetButtonInline\\\"]\")",
		"document.querySelector(\"[data-testid=\\\"tweetButton\\\"]\")",
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace = %q, want %q", trace, want)
		}
	}
}

func TestHuginnXPostPublishDriverScriptWaitsForReplyPublishURLOnOriginalStatusPage(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-x-post-publish.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "huginn-x-reply-delayed-url-trace.txt")
	statePath := filepath.Join(fixtureDir, "publish-state.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
: >"$ODIN_TEST_X_PUBLISH_STATE"
BROWSER_SERVER_URL="${BROWSER_SERVER_URL:-http://127.0.0.1:9227}"
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_health() { printf '%s' '{"ok":true,"browser":false,"page":true,"url":"https://x.com/example/status/123","title":"X"}'; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_type_selector() { printf 'type:%s:%s\n' "$1" "$2" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_click_selector() { printf 'click:%s\n' "$1" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_bc_screenshot() { printf 'screenshot:%s\n' "$*" >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_X_PUBLISH_TRACE"; }
browser_evaluate() {
    local fn="${1:-}"
    printf 'eval:%s\n' "$fn" >>"$ODIN_TEST_X_PUBLISH_TRACE"
    if [[ "$fn" == *"composer_selector"* ]]; then
        printf '%s' '{"composer_selector":"[data-testid=\"tweetTextarea_0\"]","button_selector":"[data-testid=\"tweetButtonInline\"]","current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"button_ready"* ]]; then
        printf '%s' '{"button_ready":true,"current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"clicked"* ]]; then
        printf '%s' '{"clicked":true,"current_url":"https://x.com/example/status/123","title":"X"}'
        return 0
    fi
    if [[ "$fn" == *"publish_url"* ]]; then
        local state=0
        if [[ -f "$ODIN_TEST_X_PUBLISH_STATE" ]]; then
            state="$(cat "$ODIN_TEST_X_PUBLISH_STATE" 2>/dev/null || printf '0')"
        fi
        case "$state" in
            0)
                printf '1' >"$ODIN_TEST_X_PUBLISH_STATE"
                printf '%s' '{"publish_url":"","final_url":"https://x.com/example/status/123","title":"X"}'
                ;;
            1)
                printf '2' >"$ODIN_TEST_X_PUBLISH_STATE"
                printf '%s' '{"publish_url":"","final_url":"https://x.com/example/status/123","title":"X"}'
                ;;
            *)
                printf '%s' '{"publish_url":"https://x.com/marcus/status/888888891","final_url":"https://x.com/example/status/123","title":"X"}'
                ;;
        esac
        return 0
    fi
    printf '{}'
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_x_post_publish","input":{"post_text":"Short, useful reply text.","content_kind":"reply","in_reply_to_url":"https://x.com/example/status/123","label":"social-outcome-36","wait_ms":"0"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH": browserLib,
		"ODIN_TEST_X_PUBLISH_TRACE":    tracePath,
		"ODIN_TEST_X_PUBLISH_STATE":    statePath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed (summary=%q)", response.Status, response.Summary)
	}
	if response.Artifacts["publish_url"] != "https://x.com/marcus/status/888888891" {
		t.Fatalf("publish_url = %#v, want delayed reply url", response.Artifacts["publish_url"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	if strings.Count(trace, "publish_url") < 2 {
		t.Fatalf("trace = %q, want repeated publish url polling after the first target-page readback", trace)
	}
}

func TestHuginnXPostEvidenceDriverScriptPrefersTrustedSessionWhenHeaded(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-x-post-evidence.sh")
	fixtureDir := t.TempDir()
	browserLib := filepath.Join(fixtureDir, "browser-access.sh")
	tracePath := filepath.Join(fixtureDir, "x-evidence-trace.txt")
	screenshotPath := filepath.Join(fixtureDir, "social-outcome-9.png")
	statePath := filepath.Join(fixtureDir, "x-evidence-state.txt")

	if err := os.WriteFile(browserLib, []byte(`#!/usr/bin/env bash
set -euo pipefail
browser_trusted_session_start() { printf 'trusted:%s\n' "$*" >>"$ODIN_TEST_X_EVIDENCE_TRACE"; printf 'ready\n' >"$ODIN_TEST_X_EVIDENCE_STATE"; }
browser_server_start() { printf 'start:%s\n' "$*" >>"$ODIN_TEST_X_EVIDENCE_TRACE"; printf 'raw-start\n' >"$ODIN_TEST_X_EVIDENCE_STATE"; }
browser_server_health() {
    if [[ -f "$ODIN_TEST_X_EVIDENCE_STATE" ]] && grep -q '^ready$' "$ODIN_TEST_X_EVIDENCE_STATE"; then
        printf '%s' '{"ok":true,"browser":false,"page":true,"url":"https://x.com/marcus/status/123","title":"X"}'
    else
        printf '%s' '{"ok":true,"browser":false,"page":false,"url":"","title":""}'
    fi
}
browser_snapshot() {
    if [[ -f "$ODIN_TEST_X_EVIDENCE_STATE" ]] && grep -q '^ready$' "$ODIN_TEST_X_EVIDENCE_STATE"; then
        printf '%s' 'Students do not need more motivation'
    fi
}
browser_bc_screenshot() {
    if [[ -f "$ODIN_TEST_X_EVIDENCE_STATE" ]] && grep -q '^ready$' "$ODIN_TEST_X_EVIDENCE_STATE"; then
        printf '%s' "$ODIN_TEST_X_EVIDENCE_SCREENSHOT"
        return 0
    fi
    return 1
}
browser_evaluate() {
    if [[ -f "$ODIN_TEST_X_EVIDENCE_STATE" ]] && grep -q '^ready$' "$ODIN_TEST_X_EVIDENCE_STATE"; then
        printf '%s' '{"final_url":"https://x.com/marcus/status/123","title":"X","post_text":"Students do not need more motivation","author_display_name":"Marcus Gollahon","author_handle":"@marcus","timestamp":"2026-04-23T01:18:19Z","reply_count":"4","repost_count":"2","like_count":"18","bookmark_count":"1","view_count":"1400"}'
        return 0
    fi
    printf '{}'
}
browser_server_stop() { printf 'stop\n' >>"$ODIN_TEST_X_EVIDENCE_TRACE"; }
`), 0o755); err != nil {
		t.Fatalf("WriteFile(browserLib) error = %v", err)
	}

	request := `{"tool_key":"browser_x_post_visible_evidence","input":{"target_url":"https://x.com/marcus/status/123","label":"social-outcome-9","screenshot_path":"` + screenshotPath + `","headless":"false"}}`
	response := runDriverScript(t, scriptPath, request, map[string]string{
		"ODIN_BROWSER_ACCESS_LIB_PATH":    browserLib,
		"ODIN_TEST_X_EVIDENCE_TRACE":      tracePath,
		"ODIN_TEST_X_EVIDENCE_STATE":      statePath,
		"ODIN_TEST_X_EVIDENCE_SCREENSHOT": screenshotPath,
	})

	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Artifacts["final_url"] != "https://x.com/marcus/status/123" {
		t.Fatalf("final_url = %#v, want published x url", response.Artifacts["final_url"])
	}
	if response.Artifacts["screenshot_path"] != screenshotPath {
		t.Fatalf("screenshot_path = %#v, want fixture screenshot path", response.Artifacts["screenshot_path"])
	}
	if response.Artifacts["launch_mode"] != "--headed" {
		t.Fatalf("launch_mode = %#v, want --headed", response.Artifacts["launch_mode"])
	}
	if response.Artifacts["snapshot_path"] == "" {
		t.Fatalf("snapshot_path = %#v, want non-empty snapshot path", response.Artifacts["snapshot_path"])
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	if !strings.Contains(trace, "trusted:--url https://x.com/marcus/status/123\n") {
		t.Fatalf("trace = %q, want trusted session start", trace)
	}
	if strings.Contains(trace, "start:") {
		t.Fatalf("trace = %q, want no raw browser_server_start fallback", trace)
	}
}

func TestCodexHeadlessDriverScriptPassesTaskPromptToCodexExec(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "codex-headless.sh")
	fixtureDir := t.TempDir()
	codexBin := filepath.Join(fixtureDir, "codex")
	promptPath := filepath.Join(fixtureDir, "codex-prompt.txt")

	if err := os.WriteFile(codexBin, []byte(`#!/usr/bin/env bash
set -euo pipefail
output_file=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        exec|--full-auto)
            shift
            ;;
        --cd|-C|-o|--output-last-message)
            if [[ "$1" == "-o" || "$1" == "--output-last-message" ]]; then
                output_file="$2"
            fi
            shift 2
            ;;
        -)
            shift
            ;;
        *)
            shift
            ;;
    esac
done
prompt="$(cat)"
printf '%s' "$prompt" >"$ODIN_TEST_CODEX_PROMPT"
printf 'fixture codex summary' >"$output_file"
`), 0o755); err != nil {
		t.Fatalf("WriteFile(codexBin) error = %v", err)
	}

	request := `{"operation":"run_task","task":{"ID":"task-1","Kind":"general","Scope":"project","Prompt":"Investigate the issue","Metadata":{"project_key":"family-ops","worktree_path":"` + fixtureDir + `","branch_name":"odin/task-1"}}}`
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Stdin = bytes.NewBufferString(request)
	cmd.Env = append(os.Environ(),
		"ODIN_CODEX_BIN="+codexBin,
		"ODIN_TEST_CODEX_PROMPT="+promptPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s error = %v\n%s", scriptPath, err, string(output))
	}

	var response codexDriverResponse
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("%s output json error = %v\n%s", scriptPath, err, string(output))
	}
	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Output != "fixture codex summary" {
		t.Fatalf("Output = %q, want fixture codex summary", response.Output)
	}

	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("ReadFile(promptPath) error = %v", err)
	}
	prompt := string(promptBytes)
	for _, want := range []string{"Investigate the issue", "family-ops", "odin/task-1"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt = %q, want %q", prompt, want)
		}
	}
}

func TestCodexHeadlessDriverScriptUsesContentModeForMarcusSocialDrafting(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "codex-headless.sh")
	fixtureDir := t.TempDir()
	codexBin := filepath.Join(fixtureDir, "codex")
	promptPath := filepath.Join(fixtureDir, "codex-prompt.txt")
	argsPath := filepath.Join(fixtureDir, "codex-args.txt")

	if err := os.WriteFile(codexBin, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >"$ODIN_TEST_CODEX_ARGS"
output_file=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o|--output-last-message)
            output_file="$2"
            shift 2
            ;;
        *)
            shift
            ;;
    esac
done
prompt="$(cat)"
printf '%s' "$prompt" >"$ODIN_TEST_CODEX_PROMPT"
printf 'fixture social draft output' >"$output_file"
`), 0o755); err != nil {
		t.Fatalf("WriteFile(codexBin) error = %v", err)
	}

	requestPayload := map[string]any{
		"operation": "run_task",
		"task": map[string]any{
			"ID":    "task-1",
			"Kind":  "general",
			"Scope": "global",
			"Prompt": strings.TrimSpace(`
Use the selected Odin workflow while completing this task.
Workflow: Marcus Social Growth Workflow (marcus-social-growth-workflow)
Workflow Summary: Coordinates compliant planning, drafting, review, approval, publishing, and retrospective work for Marcus's aviation authority growth on X and LinkedIn.

Use the selected Odin skill while completing this task.
Skill: X Drafting Assistant (marcus-x-drafting-assistant)
Skill Summary: Drafts concise, useful X posts for Marcus about aviation, training, and pilot development.

Task Request:
Draft one primary X post only. Topic: debrief discipline after crosswind lessons.`),
			"Metadata": map[string]string{
				"project_key":   "odin-os",
				"worktree_path": fixtureDir,
				"branch_name":   "odin/task-1",
			},
		},
	}
	requestBytes, err := json.Marshal(requestPayload)
	if err != nil {
		t.Fatalf("json.Marshal(requestPayload) error = %v", err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Stdin = bytes.NewBuffer(requestBytes)
	cmd.Env = append(os.Environ(),
		"ODIN_CODEX_BIN="+codexBin,
		"ODIN_TEST_CODEX_PROMPT="+promptPath,
		"ODIN_TEST_CODEX_ARGS="+argsPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s error = %v\n%s", scriptPath, err, string(output))
	}

	var response codexDriverResponse
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("%s output json error = %v\n%s", scriptPath, err, string(output))
	}
	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Output != "fixture social draft output" {
		t.Fatalf("Output = %q, want fixture social draft output", response.Output)
	}

	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("ReadFile(promptPath) error = %v", err)
	}
	prompt := string(promptBytes)
	for _, want := range []string{
		"This is a self-contained end-user content task inside Odin, not a repository engineering task.",
		"Do not inspect the repository, read local files, run tests, or invoke software-engineering process skills.",
		"Return only the user-facing deliverable requested in Task Request.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt = %q, want %q", prompt, want)
		}
	}

	argsBytes, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("ReadFile(argsPath) error = %v", err)
	}
	args := string(argsBytes)
	for _, want := range []string{"--skip-git-repo-check\n", "--ignore-rules\n", "--ephemeral\n"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args = %q, want %q", args, want)
		}
	}
	if strings.Contains(args, fixtureDir+"\n") {
		t.Fatalf("args = %q, do not want content mode to run in the repo worktree", args)
	}
}

func TestCodexHeadlessDriverScriptUsesConfiguredSandboxMode(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "codex-headless.sh")
	fixtureDir := t.TempDir()
	codexBin := filepath.Join(fixtureDir, "codex")
	argsPath := filepath.Join(fixtureDir, "codex-args.txt")

	if err := os.WriteFile(codexBin, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >"$ODIN_TEST_CODEX_ARGS"
output_file=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o|--output-last-message)
            output_file="$2"
            shift 2
            ;;
        *)
            shift
            ;;
    esac
done
cat >/dev/null
printf 'fixture codex summary' >"$output_file"
`), 0o755); err != nil {
		t.Fatalf("WriteFile(codexBin) error = %v", err)
	}

	request := `{"operation":"run_task","task":{"ID":"task-1","Kind":"general","Scope":"project","Prompt":"Investigate the issue","Metadata":{"project_key":"family-ops","worktree_path":"` + fixtureDir + `","branch_name":"odin/task-1"}}}`
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Stdin = bytes.NewBufferString(request)
	cmd.Env = append(os.Environ(),
		"ODIN_CODEX_BIN="+codexBin,
		"ODIN_TEST_CODEX_ARGS="+argsPath,
		"ODIN_CODEX_SANDBOX_MODE=read-only",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s error = %v\n%s", scriptPath, err, string(output))
	}

	argsBytes, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("ReadFile(argsPath) error = %v", err)
	}
	args := string(argsBytes)
	if !strings.Contains(args, "--sandbox\nread-only\n") {
		t.Fatalf("args = %q, want explicit read-only sandbox", args)
	}
	if strings.Contains(args, "--full-auto\n") {
		t.Fatalf("args = %q, do not want --full-auto when sandbox mode is explicit", args)
	}
}

func TestCodexHeadlessDriverScriptRejectsDangerFullAccess(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "codex-headless.sh")
	fixtureDir := t.TempDir()
	codexBin := filepath.Join(fixtureDir, "codex")
	argsPath := filepath.Join(fixtureDir, "codex-args.txt")

	if err := os.WriteFile(codexBin, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >"$ODIN_TEST_CODEX_ARGS"
output_file=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o|--output-last-message)
            output_file="$2"
            shift 2
            ;;
        *)
            shift
            ;;
    esac
done
cat >/dev/null
printf 'fixture codex summary' >"$output_file"
`), 0o755); err != nil {
		t.Fatalf("WriteFile(codexBin) error = %v", err)
	}

	request := `{"operation":"run_task","task":{"ID":"task-1","Kind":"general","Scope":"project","Prompt":"Investigate the issue","Metadata":{"project_key":"family-ops","worktree_path":"` + fixtureDir + `","branch_name":"odin/task-1"}}}`
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Stdin = bytes.NewBufferString(request)
	cmd.Env = append(os.Environ(),
		"ODIN_CODEX_BIN="+codexBin,
		"ODIN_TEST_CODEX_ARGS="+argsPath,
		"ODIN_CODEX_SANDBOX_MODE=danger-full-access",
	)

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s succeeded with danger-full-access, want rejection\n%s", scriptPath, string(output))
	}
	if !strings.Contains(string(output), "danger-full-access") {
		t.Fatalf("%s output = %q, want danger-full-access rejection", scriptPath, string(output))
	}
	if _, err := os.Stat(argsPath); !os.IsNotExist(err) {
		t.Fatalf("codex args file exists after rejected sandbox mode, want codex not invoked")
	}
}

func TestCodexHeadlessDriverScriptPromotesExactCommandExecution(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "codex-headless.sh")
	fixtureDir := t.TempDir()
	codexBin := filepath.Join(fixtureDir, "codex")
	codexMarker := filepath.Join(fixtureDir, "codex-called.txt")

	if err := os.WriteFile(codexBin, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf 'called' >"$ODIN_TEST_CODEX_CALLED"
exit 17
`), 0o755); err != nil {
		t.Fatalf("WriteFile(codexBin) error = %v", err)
	}

	exactCommand := `printf 'driver-newline-command'`
	requestPayload := map[string]any{
		"operation": "run_task",
		"task": map[string]any{
			"ID":     "task-1",
			"Kind":   "general",
			"Scope":  "project",
			"Prompt": "Investigate the issue. Execute this exact read-only command from the repo root and return only its JSON result plus one sentence interpreting it:\n" + exactCommand,
			"Metadata": map[string]string{
				"project_key":   "family-ops",
				"worktree_path": fixtureDir,
				"branch_name":   "odin/task-1",
			},
		},
	}
	requestBytes, err := json.Marshal(requestPayload)
	if err != nil {
		t.Fatalf("json.Marshal(requestPayload) error = %v", err)
	}
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Stdin = bytes.NewBuffer(requestBytes)
	cmd.Env = append(os.Environ(),
		"ODIN_CODEX_BIN="+codexBin,
		"ODIN_TEST_CODEX_CALLED="+codexMarker,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s error = %v\n%s", scriptPath, err, string(output))
	}

	var response codexDriverResponse
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("%s output json error = %v\n%s", scriptPath, err, string(output))
	}
	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Output != "driver-newline-command" {
		t.Fatalf("Output = %q, want driver-newline-command", response.Output)
	}
	if _, err := os.Stat(codexMarker); !os.IsNotExist(err) {
		t.Fatalf("codex marker exists, want newline exact command to bypass codex execution")
	}
}

func TestCodexHeadlessDriverScriptExecutesExactCommandDirectly(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "codex-headless.sh")
	fixtureDir := t.TempDir()
	codexBin := filepath.Join(fixtureDir, "codex")
	codexMarker := filepath.Join(fixtureDir, "codex-called.txt")

	if err := os.WriteFile(codexBin, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf 'called' >"$ODIN_TEST_CODEX_CALLED"
exit 17
`), 0o755); err != nil {
		t.Fatalf("WriteFile(codexBin) error = %v", err)
	}

	exactCommand := `python3 -c 'print("exact-direct-output")'`
	requestPayload := map[string]any{
		"operation": "run_task",
		"task": map[string]any{
			"ID":     "task-1",
			"Kind":   "general",
			"Scope":  "project",
			"Prompt": "Execute this exact read-only command from the repo root and return only its stdout: " + exactCommand,
			"Metadata": map[string]string{
				"project_key":   "family-ops",
				"worktree_path": fixtureDir,
				"branch_name":   "odin/task-1",
			},
		},
	}
	requestBytes, err := json.Marshal(requestPayload)
	if err != nil {
		t.Fatalf("json.Marshal(requestPayload) error = %v", err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Stdin = bytes.NewBuffer(requestBytes)
	cmd.Env = append(os.Environ(),
		"ODIN_CODEX_BIN="+codexBin,
		"ODIN_TEST_CODEX_CALLED="+codexMarker,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s error = %v\n%s", scriptPath, err, string(output))
	}

	var response codexDriverResponse
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("%s output json error = %v\n%s", scriptPath, err, string(output))
	}
	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Output != "exact-direct-output" {
		t.Fatalf("Output = %q, want exact-direct-output", response.Output)
	}
	if _, err := os.Stat(codexMarker); !os.IsNotExist(err) {
		t.Fatalf("codex marker exists, want exact command to bypass codex execution")
	}
}

func TestCodexHeadlessDriverScriptExecutesExactCommandWithoutLoginShellProfile(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "codex-headless.sh")
	fixtureDir := t.TempDir()
	homeDir := filepath.Join(fixtureDir, "home")
	profileMarker := filepath.Join(fixtureDir, "profile-sourced.txt")

	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(homeDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".bash_profile"), []byte(`#!/usr/bin/env bash
printf 'sourced' >"$ODIN_TEST_PROFILE_MARKER"
export ODIN_EXACT_COMMAND_TEST="clobbered-by-login-shell"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(.bash_profile) error = %v", err)
	}

	exactCommand := `printf '%s' "$ODIN_EXACT_COMMAND_TEST"`
	requestPayload := map[string]any{
		"operation": "run_task",
		"task": map[string]any{
			"ID":     "task-1",
			"Kind":   "general",
			"Scope":  "project",
			"Prompt": "Execute this exact read-only command from the repo root and return only its stdout: " + exactCommand,
			"Metadata": map[string]string{
				"project_key":   "family-ops",
				"worktree_path": fixtureDir,
				"branch_name":   "odin/task-1",
			},
		},
	}
	requestBytes, err := json.Marshal(requestPayload)
	if err != nil {
		t.Fatalf("json.Marshal(requestPayload) error = %v", err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Stdin = bytes.NewBuffer(requestBytes)
	cmd.Env = append(os.Environ(),
		"HOME="+homeDir,
		"ODIN_EXACT_COMMAND_TEST=expected-exact-output",
		"ODIN_TEST_PROFILE_MARKER="+profileMarker,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s error = %v\n%s", scriptPath, err, string(output))
	}

	var response codexDriverResponse
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("%s output json error = %v\n%s", scriptPath, err, string(output))
	}
	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.Output != "expected-exact-output" {
		t.Fatalf("Output = %q, want expected-exact-output", response.Output)
	}
	if _, err := os.Stat(profileMarker); !os.IsNotExist(err) {
		t.Fatalf("profile marker exists, want exact command execution to bypass login-shell profile loading")
	}
}

func TestBrowserAccessTrustedSessionConnectsToChromeCDP(t *testing.T) {
	repoRoot := projectRoot(t)
	browserAccessPath := filepath.Join(repoRoot, "scripts", "browser", "browser-access.sh")
	if _, err := os.Stat(browserAccessPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", browserAccessPath, err)
	}

	fixtureDir := t.TempDir()
	cdpLib := filepath.Join(fixtureDir, "chrome-cdp-start.sh")
	tracePath := filepath.Join(fixtureDir, "cdp-trace.txt")

	if err := os.WriteFile(cdpLib, []byte(`#!/usr/bin/env bash
set -euo pipefail

cdp_start() {
    printf 'start\n' >>"$ODIN_TEST_CDP_TRACE"
    export CHROME_CDP_PORT=9333
}

cdp_stop() {
    printf 'stop\n' >>"$ODIN_TEST_CDP_TRACE"
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(cdpLib) error = %v", err)
	}

	var (
		mu           sync.Mutex
		connectCount int
		launchCount  int
		stopCount    int
		connectURL   string
		cdpURL       string
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"ok":      true,
			"engine":  "chromium",
			"browser": false,
			"page":    false,
		})
	})
	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body struct {
			CDPURL string `json:"cdpUrl"`
			URL    string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode(/connect) error = %v", err)
		}
		mu.Lock()
		connectCount++
		connectURL = body.URL
		cdpURL = body.CDPURL
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"ok":  true,
			"url": body.URL,
		})
	})
	mux.HandleFunc("/launch", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		launchCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		stopCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{"ok": true})
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
	})

	cmd := exec.Command("bash", "-lc", `
source "$ODIN_TEST_BROWSER_ACCESS"
browser_trusted_session_start --url "https://dashboard.plaid.com/transfer/application"
browser_server_stop
`)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_TEST_BROWSER_ACCESS="+browserAccessPath,
		"ODIN_DIR="+fixtureDir,
		"ODIN_BROWSER_SERVER_URL=http://"+listener.Addr().String(),
		"ODIN_CHROME_CDP_LIB_PATH="+cdpLib,
		"ODIN_TEST_CDP_TRACE="+tracePath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trusted session command error = %v\n%s", err, string(output))
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	if !strings.Contains(trace, "start\n") {
		t.Fatalf("trace = %q, want cdp start", trace)
	}
	if strings.Contains(trace, "stop\n") {
		t.Fatalf("trace = %q, do not want cdp_stop when preserving trusted session", trace)
	}

	mu.Lock()
	defer mu.Unlock()
	if connectCount != 1 {
		t.Fatalf("connectCount = %d, want 1", connectCount)
	}
	if launchCount != 0 {
		t.Fatalf("launchCount = %d, want 0 when trusted CDP connect is used", launchCount)
	}
	if stopCount != 0 {
		t.Fatalf("stopCount = %d, want 0 when trusted session cleanup preserves server", stopCount)
	}
	if cdpURL != "http://127.0.0.1:9333" {
		t.Fatalf("cdpURL = %q, want trusted local Chrome CDP url", cdpURL)
	}
	if connectURL != "https://dashboard.plaid.com/transfer/application" {
		t.Fatalf("connectURL = %q, want Plaid application url", connectURL)
	}
}

func TestBrowserAccessTrustedSessionReusesAttachedPageWithoutReconnect(t *testing.T) {
	repoRoot := projectRoot(t)
	browserAccessPath := filepath.Join(repoRoot, "scripts", "browser", "browser-access.sh")
	if _, err := os.Stat(browserAccessPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", browserAccessPath, err)
	}

	fixtureDir := t.TempDir()
	cdpLib := filepath.Join(fixtureDir, "chrome-cdp-start.sh")
	tracePath := filepath.Join(fixtureDir, "cdp-trace.txt")

	if err := os.WriteFile(cdpLib, []byte(`#!/usr/bin/env bash
set -euo pipefail

cdp_start() {
    printf 'start\n' >>"$ODIN_TEST_CDP_TRACE"
    export CHROME_CDP_PORT=9333
}

cdp_stop() {
    printf 'stop\n' >>"$ODIN_TEST_CDP_TRACE"
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(cdpLib) error = %v", err)
	}

	var (
		mu            sync.Mutex
		healthCount   int
		evaluateCount int
		connectCount  int
		navigateCount int
		stopCount     int
		navigateURL   string
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		healthCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"ok":      true,
			"engine":  "chromium",
			"browser": false,
			"page":    true,
			"url":     "https://x.com/compose/post",
			"title":   "Home / X",
		})
	})
	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connectCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/evaluate", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		evaluateCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"ok":     true,
			"result": map[string]any{"url": "https://x.com/compose/post", "title": "Home / X"},
			"url":    "https://x.com/compose/post",
			"title":  "Home / X",
		})
	})
	mux.HandleFunc("/navigate", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode(/navigate) error = %v", err)
		}
		mu.Lock()
		navigateCount++
		navigateURL = body.URL
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"ok":    true,
			"url":   body.URL,
			"title": "Home / X",
		})
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		stopCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{"ok": true})
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
	})

	cmd := exec.Command("bash", "-lc", `
source "$ODIN_TEST_BROWSER_ACCESS"
browser_trusted_session_start --url "https://x.com/compose/post"
browser_server_stop
`)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_TEST_BROWSER_ACCESS="+browserAccessPath,
		"ODIN_DIR="+fixtureDir,
		"ODIN_BROWSER_SERVER_URL=http://"+listener.Addr().String(),
		"ODIN_CHROME_CDP_LIB_PATH="+cdpLib,
		"ODIN_TEST_CDP_TRACE="+tracePath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trusted session reuse command error = %v\n%s", err, string(output))
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	if trace != "" {
		t.Fatalf("trace = %q, want no cdp start/stop when reusing attached page", trace)
	}

	mu.Lock()
	defer mu.Unlock()
	if healthCount == 0 {
		t.Fatalf("healthCount = 0, want trusted session reuse to check server health")
	}
	if evaluateCount != 1 {
		t.Fatalf("evaluateCount = %d, want 1 responsive page probe before reuse", evaluateCount)
	}
	if connectCount != 0 {
		t.Fatalf("connectCount = %d, want no reconnect when page is already attached", connectCount)
	}
	if navigateCount != 1 {
		t.Fatalf("navigateCount = %d, want 1 when reusing attached page", navigateCount)
	}
	if navigateURL != "https://x.com/compose/post" {
		t.Fatalf("navigateURL = %q, want compose url", navigateURL)
	}
	if stopCount != 0 {
		t.Fatalf("stopCount = %d, want trusted cleanup to preserve attached server", stopCount)
	}
}

func TestBrowserAccessTrustedSessionReconnectsWhenAttachedPageProbeFails(t *testing.T) {
	repoRoot := projectRoot(t)
	browserAccessPath := filepath.Join(repoRoot, "scripts", "browser", "browser-access.sh")
	if _, err := os.Stat(browserAccessPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", browserAccessPath, err)
	}

	fixtureDir := t.TempDir()
	cdpLib := filepath.Join(fixtureDir, "chrome-cdp-start.sh")
	tracePath := filepath.Join(fixtureDir, "cdp-trace.txt")

	if err := os.WriteFile(cdpLib, []byte(`#!/usr/bin/env bash
set -euo pipefail

cdp_start() {
    printf 'start\n' >>"$ODIN_TEST_CDP_TRACE"
    export CHROME_CDP_PORT=9333
}

cdp_stop() {
    printf 'stop\n' >>"$ODIN_TEST_CDP_TRACE"
}
`), 0o755); err != nil {
		t.Fatalf("WriteFile(cdpLib) error = %v", err)
	}

	var (
		mu            sync.Mutex
		healthCount   int
		evaluateCount int
		connectCount  int
		navigateCount int
		stopCount     int
		connectURL    string
		cdpURL        string
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		healthCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"ok":      true,
			"engine":  "chromium",
			"browser": false,
			"page":    true,
			"url":     "https://x.com/compose/post",
			"title":   "Home / X",
		})
	})
	mux.HandleFunc("/evaluate", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		evaluateCount++
		mu.Unlock()
		time.Sleep(1500 * time.Millisecond)
		writeJSON(t, w, map[string]any{
			"ok":     true,
			"result": map[string]any{"url": "https://x.com/compose/post", "title": "Home / X"},
			"url":    "https://x.com/compose/post",
			"title":  "Home / X",
		})
	})
	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body struct {
			CDPURL string `json:"cdpUrl"`
			URL    string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode(/connect) error = %v", err)
		}
		mu.Lock()
		connectCount++
		connectURL = body.URL
		cdpURL = body.CDPURL
		mu.Unlock()
		writeJSON(t, w, map[string]any{"ok": true, "url": body.URL})
	})
	mux.HandleFunc("/navigate", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		navigateCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		stopCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{"ok": true})
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
	})

	cmd := exec.Command("bash", "-lc", `
source "$ODIN_TEST_BROWSER_ACCESS"
browser_trusted_session_start --url "https://x.com/compose/post"
browser_server_stop
`)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_TEST_BROWSER_ACCESS="+browserAccessPath,
		"ODIN_DIR="+fixtureDir,
		"ODIN_BROWSER_SERVER_URL=http://"+listener.Addr().String(),
		"ODIN_CHROME_CDP_LIB_PATH="+cdpLib,
		"ODIN_TEST_CDP_TRACE="+tracePath,
		"ODIN_BROWSER_PROBE_CURL_MAX_TIME=1",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trusted session stale probe command error = %v\n%s", err, string(output))
	}

	traceBytes, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile(tracePath) error = %v", err)
	}
	trace := string(traceBytes)
	if !strings.Contains(trace, "start\n") {
		t.Fatalf("trace = %q, want cdp start after failed reuse probe", trace)
	}
	if strings.Contains(trace, "stop\n") {
		t.Fatalf("trace = %q, do not want cdp stop when preserving trusted session", trace)
	}

	mu.Lock()
	defer mu.Unlock()
	if healthCount == 0 {
		t.Fatalf("healthCount = 0, want trusted session to check server health")
	}
	if evaluateCount != 1 {
		t.Fatalf("evaluateCount = %d, want one failed page probe before reconnect", evaluateCount)
	}
	if connectCount != 1 {
		t.Fatalf("connectCount = %d, want reconnect when attached page probe fails", connectCount)
	}
	if navigateCount != 0 {
		t.Fatalf("navigateCount = %d, want no navigate reuse after failed probe", navigateCount)
	}
	if stopCount != 0 {
		t.Fatalf("stopCount = %d, want trusted cleanup to preserve attached server", stopCount)
	}
	if cdpURL != "http://127.0.0.1:9333" {
		t.Fatalf("cdpURL = %q, want trusted local Chrome CDP url", cdpURL)
	}
	if connectURL != "https://x.com/compose/post" {
		t.Fatalf("connectURL = %q, want compose url on reconnect", connectURL)
	}
}

func TestBrowserAccessClickSelectorPostsClickAction(t *testing.T) {
	repoRoot := projectRoot(t)
	browserAccessPath := filepath.Join(repoRoot, "scripts", "browser", "browser-access.sh")
	if _, err := os.Stat(browserAccessPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", browserAccessPath, err)
	}

	var (
		mu          sync.Mutex
		healthCount int
		actCount    int
		actKind     string
		actSelector string
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		healthCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"ok":      true,
			"engine":  "chromium",
			"browser": true,
			"page":    true,
			"url":     "https://dashboard.plaid.com/support/new/admin/account-administration",
		})
	})
	mux.HandleFunc("/act", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body struct {
			Kind     string `json:"kind"`
			Selector string `json:"selector"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode(/act) error = %v", err)
		}
		mu.Lock()
		actCount++
		actKind = body.Kind
		actSelector = body.Selector
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"ok":    true,
			"url":   "https://dashboard.plaid.com/support/new/admin/account-administration",
			"title": "Plaid - Dashboard",
		})
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
	})

	cmd := exec.Command("bash", "-lc", `
source "$ODIN_TEST_BROWSER_ACCESS"
browser_click_selector "#secondaryCategoryDropdown .css-1012nnc-control"
`)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_TEST_BROWSER_ACCESS="+browserAccessPath,
		"ODIN_BROWSER_SERVER_URL=http://"+listener.Addr().String(),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser_click_selector command error = %v\n%s", err, string(output))
	}

	mu.Lock()
	defer mu.Unlock()
	if healthCount == 0 {
		t.Fatalf("healthCount = 0, want browser helper to check server health")
	}
	if actCount != 1 {
		t.Fatalf("actCount = %d, want 1", actCount)
	}
	if actKind != "click" {
		t.Fatalf("actKind = %q, want click", actKind)
	}
	if actSelector != "#secondaryCategoryDropdown .css-1012nnc-control" {
		t.Fatalf("actSelector = %q, want selector payload", actSelector)
	}
}

func TestBrowserAccessNavigatePostsNavigateRequest(t *testing.T) {
	repoRoot := projectRoot(t)
	browserAccessPath := filepath.Join(repoRoot, "scripts", "browser", "browser-access.sh")
	if _, err := os.Stat(browserAccessPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", browserAccessPath, err)
	}

	var (
		mu            sync.Mutex
		healthCount   int
		navigateCount int
		navigateURL   string
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		healthCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"ok":      true,
			"engine":  "chromium",
			"browser": true,
			"page":    true,
			"url":     "https://dashboard.plaid.com/support/new/admin/account-administration",
		})
	})
	mux.HandleFunc("/navigate", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode(/navigate) error = %v", err)
		}
		mu.Lock()
		navigateCount++
		navigateURL = body.URL
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"ok":    true,
			"url":   body.URL,
			"title": "Plaid - Dashboard",
		})
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
	})

	cmd := exec.Command("bash", "-lc", `
source "$ODIN_TEST_BROWSER_ACCESS"
browser_navigate "https://dashboard.plaid.com/support/new/admin/account-administration/pricing"
`)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_TEST_BROWSER_ACCESS="+browserAccessPath,
		"ODIN_BROWSER_SERVER_URL=http://"+listener.Addr().String(),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser_navigate command error = %v\n%s", err, string(output))
	}

	mu.Lock()
	defer mu.Unlock()
	if healthCount == 0 {
		t.Fatalf("healthCount = 0, want browser helper to check server health")
	}
	if navigateCount != 1 {
		t.Fatalf("navigateCount = %d, want 1", navigateCount)
	}
	if navigateURL != "https://dashboard.plaid.com/support/new/admin/account-administration/pricing" {
		t.Fatalf("navigateURL = %q, want target url", navigateURL)
	}
}

func TestChromeCDPStartChoosesFreePortWhenConfiguredPortIsBusy(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "browser", "chrome-cdp-start.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", scriptPath, err)
	}

	fixtureDir := t.TempDir()
	fakeChrome := filepath.Join(fixtureDir, "fake-chrome.sh")
	argsPath := filepath.Join(fixtureDir, "fake-chrome-args.txt")
	readyPath := filepath.Join(fixtureDir, "fake-chrome-ready")
	if err := os.WriteFile(fakeChrome, []byte(`#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >"$ODIN_TEST_FAKE_CHROME_ARGS"
touch "$ODIN_TEST_FAKE_CHROME_READY"
sleep 30
`), 0o755); err != nil {
		t.Fatalf("WriteFile(fakeChrome) error = %v", err)
	}

	busyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = busyListener.Close() })
	busyPort := busyListener.Addr().(*net.TCPAddr).Port

	cmd := exec.Command("bash", "-lc", `
source "$ODIN_TEST_CHROME_CDP_START"
cdp_is_ready() {
    [[ -f "$ODIN_TEST_FAKE_CHROME_READY" ]]
}
if cdp_start; then
    echo start=ok
    echo configured_port="$ODIN_CHROME_CDP_PORT"
    echo actual_port="$CHROME_CDP_PORT"
    echo url="$(cdp_url)"
    cdp_stop >/dev/null 2>&1 || true
else
    echo start=fail
fi
`)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_TEST_CHROME_CDP_START="+scriptPath,
		"ODIN_DIR="+fixtureDir,
		"ODIN_CHROME_CDP_PORT="+strconv.Itoa(busyPort),
		"ODIN_CHROME_DISPLAY=:99",
		"CHROME_BIN="+fakeChrome,
		"ODIN_TEST_FAKE_CHROME_ARGS="+argsPath,
		"ODIN_TEST_FAKE_CHROME_READY="+readyPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("chrome cdp command error = %v\n%s", err, string(output))
	}

	text := string(output)
	if !strings.Contains(text, "start=ok") {
		t.Fatalf("output = %q, want cdp_start success", text)
	}
	if !strings.Contains(text, "configured_port="+strconv.Itoa(busyPort)) {
		t.Fatalf("output = %q, want configured busy port", text)
	}
	if strings.Contains(text, "actual_port="+strconv.Itoa(busyPort)) {
		t.Fatalf("output = %q, want fallback to a different free port", text)
	}

	argsBytes, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("ReadFile(argsPath) error = %v", err)
	}
	args := string(argsBytes)
	if strings.Contains(args, "--remote-debugging-port="+strconv.Itoa(busyPort)) {
		t.Fatalf("fake chrome args = %q, want a different remote debugging port than the busy configured port", args)
	}
}

func TestChromeCDPStartClearsStaleSingletonLocksBeforeLaunch(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "browser", "chrome-cdp-start.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", scriptPath, err)
	}

	fixtureDir := t.TempDir()
	fakeChrome := filepath.Join(fixtureDir, "fake-chrome.sh")
	readyPath := filepath.Join(fixtureDir, "fake-chrome-ready")
	profileDir := filepath.Join(fixtureDir, "browser-state", "chrome-profile")
	lockPath := filepath.Join(profileDir, "SingletonLock")
	cookiePath := filepath.Join(profileDir, "SingletonCookie")
	socketPath := filepath.Join(profileDir, "SingletonSocket")
	logDir := filepath.Join(fixtureDir, "logs", time.Now().UTC().Format("2006-01-02"))

	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(profileDir) error = %v", err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(logDir) error = %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("stale-lock"), 0o644); err != nil {
		t.Fatalf("WriteFile(lockPath) error = %v", err)
	}
	if err := os.WriteFile(cookiePath, []byte("stale-cookie"), 0o644); err != nil {
		t.Fatalf("WriteFile(cookiePath) error = %v", err)
	}
	if err := os.WriteFile(socketPath, []byte("stale-socket"), 0o644); err != nil {
		t.Fatalf("WriteFile(socketPath) error = %v", err)
	}

	if err := os.WriteFile(fakeChrome, []byte(`#!/usr/bin/env bash
set -euo pipefail

profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*)
      profile="${arg#--user-data-dir=}"
      ;;
  esac
done

if [[ -e "${profile}/SingletonLock" || -e "${profile}/SingletonCookie" || -e "${profile}/SingletonSocket" ]]; then
  printf 'stale singleton files still present\n' >&2
  exit 23
fi

touch "$ODIN_TEST_FAKE_CHROME_READY"
sleep 30
`), 0o755); err != nil {
		t.Fatalf("WriteFile(fakeChrome) error = %v", err)
	}

	cmd := exec.Command("bash", "-lc", `
source "$ODIN_TEST_CHROME_CDP_START"
cdp_is_ready() {
    [[ -f "$ODIN_TEST_FAKE_CHROME_READY" ]]
}
if cdp_start; then
    echo start=ok
    cdp_stop >/dev/null 2>&1 || true
else
    echo start=fail
fi
`)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_TEST_CHROME_CDP_START="+scriptPath,
		"ODIN_DIR="+fixtureDir,
		"CHROME_BIN="+fakeChrome,
		"ODIN_TEST_FAKE_CHROME_READY="+readyPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("chrome cdp singleton cleanup command error = %v\n%s", err, string(output))
	}

	text := string(output)
	if !strings.Contains(text, "start=ok") {
		t.Fatalf("output = %q, want cdp_start success after stale singleton cleanup", text)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("SingletonLock still exists after cdp_start")
	}
	if _, err := os.Stat(cookiePath); !os.IsNotExist(err) {
		t.Fatalf("SingletonCookie still exists after cdp_start")
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("SingletonSocket still exists after cdp_start")
	}
}

func TestChromeCDPStartReusesExistingProfilePortWhenTrustedBrowserAlreadyRunning(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "browser", "chrome-cdp-start.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", scriptPath, err)
	}

	fixtureDir := t.TempDir()
	fakeChrome := filepath.Join(fixtureDir, "fake-chrome.sh")
	calledMarker := filepath.Join(fixtureDir, "fake-chrome-called")
	profileDir := filepath.Join(fixtureDir, "browser-state", "chrome-profile")

	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(profileDir) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(fixtureDir, "logs", time.Now().UTC().Format("2006-01-02")), 0o755); err != nil {
		t.Fatalf("MkdirAll(logDir) error = %v", err)
	}
	if err := os.WriteFile(fakeChrome, []byte(`#!/usr/bin/env bash
set -euo pipefail
printf 'called' >"$ODIN_TEST_FAKE_CHROME_CALLED"
exit 99
`), 0o755); err != nil {
		t.Fatalf("WriteFile(fakeChrome) error = %v", err)
	}

	cmd := exec.Command("bash", "-lc", `
source "$ODIN_TEST_CHROME_CDP_START"
pgrep() {
    if [[ "${1:-}" == "-f" || "${1:-}" == "-af" ]]; then
        printf '1234 /usr/bin/google-chrome --remote-debugging-port=33223 --user-data-dir='"$ODIN_DIR"'/browser-state/chrome-profile about:blank\n'
        return 0
    fi
    return 1
}
cdp_is_ready() {
    [[ "${CHROME_CDP_PORT:-}" == "33223" ]]
}
if cdp_start; then
    echo start=ok
    echo actual_port="$CHROME_CDP_PORT"
    echo url="$(cdp_url)"
else
    echo start=fail
fi
`)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_TEST_CHROME_CDP_START="+scriptPath,
		"ODIN_DIR="+fixtureDir,
		"CHROME_BIN="+fakeChrome,
		"ODIN_TEST_FAKE_CHROME_CALLED="+calledMarker,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("chrome cdp reuse command error = %v\n%s", err, string(output))
	}

	text := string(output)
	if !strings.Contains(text, "start=ok") {
		t.Fatalf("output = %q, want cdp_start success", text)
	}
	if !strings.Contains(text, "actual_port=33223") {
		t.Fatalf("output = %q, want reused existing profile port", text)
	}
	if !strings.Contains(text, "url=http://127.0.0.1:33223") {
		t.Fatalf("output = %q, want reused cdp url", text)
	}
	if _, err := os.Stat(calledMarker); !os.IsNotExist(err) {
		t.Fatalf("fake chrome was called, want cdp_start to reuse existing trusted browser")
	}
}

func TestChromeCDPStartCreatesDatedLogDirectoryWhenMissing(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "browser", "chrome-cdp-start.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", scriptPath, err)
	}

	fixtureDir := t.TempDir()
	fakeChrome := filepath.Join(fixtureDir, "fake-chrome.sh")
	readyPath := filepath.Join(fixtureDir, "fake-chrome-ready")
	logDir := filepath.Join(fixtureDir, "logs", time.Now().UTC().Format("2006-01-02"))
	logPath := filepath.Join(logDir, "alerts.log")

	if err := os.WriteFile(fakeChrome, []byte(`#!/usr/bin/env bash
set -euo pipefail
touch "$ODIN_TEST_FAKE_CHROME_READY"
sleep 30
`), 0o755); err != nil {
		t.Fatalf("WriteFile(fakeChrome) error = %v", err)
	}

	cmd := exec.Command("bash", "-lc", `
source "$ODIN_TEST_CHROME_CDP_START"
cdp_is_ready() {
    [[ -f "$ODIN_TEST_FAKE_CHROME_READY" ]]
}
if cdp_start; then
    echo start=ok
    cdp_stop >/dev/null 2>&1 || true
else
    echo start=fail
fi
`)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_TEST_CHROME_CDP_START="+scriptPath,
		"ODIN_DIR="+fixtureDir,
		"CHROME_BIN="+fakeChrome,
		"ODIN_TEST_FAKE_CHROME_READY="+readyPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("chrome cdp log-dir command error = %v\n%s", err, string(output))
	}

	text := string(output)
	if !strings.Contains(text, "start=ok") {
		t.Fatalf("output = %q, want cdp_start success", text)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("Stat(%s) error = %v, want alerts log created", logPath, err)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", logPath, err)
	}
	if !strings.Contains(string(logBytes), "Chrome CDP ready") {
		t.Fatalf("alerts.log = %q, want Chrome CDP ready entry", string(logBytes))
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
	env = append(env, "ODIN_HUGINN_SNAPSHOT_TEXT="+extraEnv["ODIN_TEST_HUGINN_SNAPSHOT"])
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
