package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHuginnBrowserSessionScript(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "huginn-browser-session.sh")
	assertDriverScriptShape(t, scriptPath)

	t.Run("health", func(t *testing.T) {
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "huginn-browser-session.sh", `{"tool_key":"huginn_browser_session","input":{"action":"health"}}`, map[string]string{
			"ODIN_BROWSER_STUB_HEALTH_STATE": "healthy",
		})
		assertStructuredDriverOutput(t, stdout, "huginn_browser_session", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "healthy")
		assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContains(t, callsLog, "health:")
	})

	t.Run("health stopped is failed", func(t *testing.T) {
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "huginn-browser-session.sh", `{"tool_key":"huginn_browser_session","input":{"action":"health"}}`, map[string]string{
			"ODIN_BROWSER_STUB_HEALTH_STATE": "stopped",
		})
		assertStructuredDriverOutput(t, stdout, "huginn_browser_session", "failed")
		assertJSONArtifactString(t, stdout, "session_state", "stopped")
		assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContains(t, callsLog, "health:")
	})

	t.Run("health failure closes as unhealthy", func(t *testing.T) {
		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "huginn-browser-session.sh", `{"tool_key":"huginn_browser_session","input":{"action":"health"}}`, map[string]string{
			"ODIN_BROWSER_STUB_HEALTH_EXIT_CODE": "1",
		})
		assertStructuredDriverOutput(t, stdout, "huginn_browser_session", "failed")
		assertJSONArtifactString(t, stdout, "session_state", "unhealthy")
		assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContains(t, callsLog, "health:")
	})

	t.Run("launch cleanup on navigate failure", func(t *testing.T) {
		stdout, callsLog, markerPath, err := runBrowserDriverScriptRaw(t, repoRoot, scriptPath, "huginn-browser-session.sh", `{"tool_key":"huginn_browser_session","input":{"action":"launch","url":"https://example.com"}}`, map[string]string{
			"ODIN_BROWSER_STUB_NAVIGATE_EXIT_CODE": "1",
		}, browserAccessStubContent())
		if err != nil {
			t.Fatalf("expected handled launch failure to exit 0, got err=%v\n%s", err, stdout)
		}
		assertStructuredDriverOutput(t, stdout, "huginn_browser_session", "failed")
		assertJSONArtifactString(t, stdout, "session_state", "failed")
		assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContains(t, callsLog, "request:https://example.com")
		assertFileContains(t, callsLog, "start:")
		assertFileContains(t, callsLog, "navigate:https://example.com")
		assertFileContains(t, callsLog, "stop:")
	})

	t.Run("launch snapshot screenshot stop", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "browser.png")

		stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "huginn-browser-session.sh", `{"tool_key":"huginn_browser_session","input":{"action":"launch","url":"https://example.com"}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":        "Example Domain",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH": screenshotPath,
		})
		assertStructuredDriverOutput(t, stdout, "huginn_browser_session", "completed")
		assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContains(t, callsLog, "request:https://example.com")
		assertFileContains(t, callsLog, "start:")
		assertFileContains(t, callsLog, "navigate:https://example.com")
		assertJSONArtifactString(t, stdout, "session_state", "running")

		stdout, callsLog, markerPath = runBrowserDriverScript(t, repoRoot, scriptPath, "huginn-browser-session.sh", `{"tool_key":"huginn_browser_session","input":{"action":"snapshot"}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT": "Example Domain",
		})
		assertStructuredDriverOutput(t, stdout, "huginn_browser_session", "completed")
		assertJSONArtifactString(t, stdout, "snapshot", "Example Domain")
		assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContains(t, callsLog, "snapshot:")

		var err error
		stdout, callsLog, markerPath, err = runBrowserDriverScriptRaw(t, repoRoot, scriptPath, "huginn-browser-session.sh", `{"tool_key":"huginn_browser_session","input":{"action":"screenshot","path":"`+screenshotPath+`"}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":        "Example Domain",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH": screenshotPath,
		}, browserAccessScreenshotWrapperContent(repoRoot))
		if err != nil {
			t.Fatalf("screenshot action failed: %v\n%s", err, stdout)
		}
		assertStructuredDriverOutput(t, stdout, "huginn_browser_session", "completed")
		assertJSONArtifactString(t, stdout, "screenshot_path", screenshotPath)
		assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileExists(t, screenshotPath)
		assertFileContains(t, callsLog, "screenshot:")

		stdout, callsLog, markerPath = runBrowserDriverScript(t, repoRoot, scriptPath, "huginn-browser-session.sh", `{"tool_key":"huginn_browser_session","input":{"action":"stop"}}`, nil)
		assertStructuredDriverOutput(t, stdout, "huginn_browser_session", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "stopped")
		assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContains(t, callsLog, "stop:")
	})
}

func TestPlaidTransferApplicationScript(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "plaid-transfer-application.sh")
	assertDriverScriptShape(t, scriptPath)

	cases := []struct {
		name      string
		snapshot  string
		wantState string
	}{
		{name: "ready_for_login", snapshot: "Plaid dashboard\nSign in to continue", wantState: "ready_for_login"},
		{name: "blocked_on_mfa", snapshot: "Enter the verification code from your authenticator app", wantState: "blocked_on_mfa"},
		{name: "submitted_for_review", snapshot: "Your Transfer application has been submitted for review", wantState: "submitted_for_review"},
		{name: "already_enabled", snapshot: "Transfer is already enabled for this account", wantState: "already_enabled"},
		{name: "unclassified", snapshot: "Plaid dashboard\nApplication state unavailable", wantState: "unclassified"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "plaid-transfer-application.sh", `{"tool_key":"plaid_transfer_application","input":{"action":"inspect","application_url":"https://dashboard.plaid.com/transfer/application"}}`, map[string]string{
				"ODIN_BROWSER_STUB_SNAPSHOT": tc.snapshot,
			})
			assertStructuredDriverOutput(t, stdout, "plaid_transfer_application", "completed")
			assertJSONArtifactString(t, stdout, "session_state", tc.wantState)
			assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
			assertFileContains(t, callsLog, "request:https://dashboard.plaid.com/transfer/application")
			assertFileContains(t, callsLog, "start:")
			assertFileContains(t, callsLog, "https://dashboard.plaid.com/transfer/application")
			assertFileContains(t, callsLog, "snapshot:")
			assertFileContains(t, callsLog, "screenshot:")
		})
	}

	t.Run("reject non-plaid urls", func(t *testing.T) {
		stdout, callsLog, markerPath, err := runBrowserDriverScriptRaw(t, repoRoot, scriptPath, "plaid-transfer-application.sh", `{"tool_key":"plaid_transfer_application","input":{"action":"inspect","application_url":"https://example.com/transfer/application"}}`, nil, browserAccessStubContent())
		if err != nil {
			t.Fatalf("expected handled non-Plaid url failure to exit 0, got err=%v\n%s", err, stdout)
		}
		assertStructuredDriverOutput(t, stdout, "plaid_transfer_application", "failed")
		assertJSONArtifactString(t, stdout, "session_state", "failed")
		assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileNotContains(t, callsLog, "request:")
		assertFileNotContains(t, callsLog, "start:")
		assertFileNotContains(t, callsLog, "snapshot:")
		assertFileNotContains(t, callsLog, "screenshot:")
	})
}

func TestPlaidTransferApplicationArtifacts(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "plaid-transfer-application.sh")
	assertDriverScriptShape(t, scriptPath)

	const applicationURL = "https://dashboard.plaid.com/transfer/application"

	cases := []struct {
		name           string
		snapshot       string
		wantState      string
		wantSummary    string
		wantNextAction string
	}{
		{name: "login", snapshot: "Plaid dashboard\nSign in to continue", wantState: "ready_for_login", wantSummary: "Plaid transfer workflow requires login", wantNextAction: "sign in to Plaid"},
		{name: "mfa", snapshot: "Enter the verification code from your authenticator app", wantState: "blocked_on_mfa", wantSummary: "Plaid transfer workflow is blocked on MFA", wantNextAction: "complete MFA challenge"},
		{name: "review", snapshot: "Your Transfer application has been submitted for review", wantState: "submitted_for_review", wantSummary: "Plaid transfer application is under review", wantNextAction: "wait for review"},
		{name: "enabled", snapshot: "Transfer is already enabled for this account", wantState: "already_enabled", wantSummary: "Plaid transfer application is already enabled", wantNextAction: "no action needed"},
		{name: "unclassified", snapshot: "Plaid dashboard\nApplication state unavailable", wantState: "unclassified", wantSummary: "Plaid transfer workflow is unclassified", wantNextAction: "inspect dashboard"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			screenshotPath := filepath.Join(t.TempDir(), "plaid.png")

			stdout, callsLog, markerPath := runBrowserDriverScript(t, repoRoot, scriptPath, "plaid-transfer-application.sh", `{"tool_key":"plaid_transfer_application","input":{"action":"inspect","application_url":"https://dashboard.plaid.com/transfer/application"}}`, map[string]string{
				"ODIN_BROWSER_STUB_SNAPSHOT":        tc.snapshot,
				"ODIN_BROWSER_STUB_SCREENSHOT_PATH": screenshotPath,
			})
			assertStructuredDriverOutput(t, stdout, "plaid_transfer_application", "completed")

			var payload map[string]any
			if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
				t.Fatalf("driver output is not valid json: %v\nstdout=%s", err, stdout)
			}
			artifacts, ok := payload["artifacts"].(map[string]any)
			if !ok || len(artifacts) == 0 {
				t.Fatalf("artifacts = %#v, want non-empty object", payload["artifacts"])
			}

			assertField := func(key, want string) {
				t.Helper()
				if got := stringValue(payload[key]); got != want {
					t.Fatalf("%s = %q, want %q", key, got, want)
				}
				if got := stringValue(artifacts[key]); got != want {
					t.Fatalf("artifacts[%s] = %q, want %q", key, got, want)
				}
			}

			assertField("session_state", tc.wantState)
			assertField("current_url", applicationURL)
			assertField("screenshot_path", screenshotPath)
			assertField("evidence", tc.snapshot)
			assertField("next_action", tc.wantNextAction)

			if got := stringValue(payload["summary"]); got != tc.wantSummary {
				t.Fatalf("summary = %q, want %q", got, tc.wantSummary)
			}

			assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
			assertFileContains(t, callsLog, "request:https://dashboard.plaid.com/transfer/application")
			assertFileContains(t, callsLog, "start:")
			assertFileContains(t, callsLog, applicationURL)
			assertFileContains(t, callsLog, "snapshot:")
			assertFileContains(t, callsLog, "screenshot:")
		})
	}

	t.Run("screenshot failure is best-effort", func(t *testing.T) {
		screenshotPath := filepath.Join(t.TempDir(), "plaid.png")

		stdout, callsLog, markerPath, err := runBrowserDriverScriptRaw(t, repoRoot, scriptPath, "plaid-transfer-application.sh", `{"tool_key":"plaid_transfer_application","input":{"action":"inspect","application_url":"https://dashboard.plaid.com/transfer/application"}}`, map[string]string{
			"ODIN_BROWSER_STUB_SNAPSHOT":             "Plaid dashboard\nApplication state unavailable",
			"ODIN_BROWSER_STUB_SCREENSHOT_PATH":      screenshotPath,
			"ODIN_BROWSER_STUB_SCREENSHOT_EXIT_CODE": "1",
		}, browserAccessStubContent())
		if err != nil {
			t.Fatalf("expected handled screenshot failure to exit 0, got err=%v\n%s", err, stdout)
		}
		assertStructuredDriverOutput(t, stdout, "plaid_transfer_application", "completed")
		assertJSONArtifactString(t, stdout, "session_state", "unclassified")
		assertJSONArtifactString(t, stdout, "current_url", "https://dashboard.plaid.com/transfer/application")
		assertJSONArtifactString(t, stdout, "screenshot_path", "")
		assertJSONArtifactString(t, stdout, "evidence", "Plaid dashboard\nApplication state unavailable")
		assertJSONArtifactString(t, stdout, "next_action", "inspect dashboard")

		var payload map[string]any
		if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
			t.Fatalf("driver output is not valid json: %v\nstdout=%s", err, stdout)
		}
		if got := stringValue(payload["summary"]); got != "Plaid transfer workflow is unclassified" {
			t.Fatalf("summary = %q, want %q", got, "Plaid transfer workflow is unclassified")
		}
		assertFileContains(t, markerPath, "sourced repo-local browser-access.sh")
		assertFileContains(t, callsLog, "request:https://dashboard.plaid.com/transfer/application")
		assertFileContains(t, callsLog, "start:")
		assertFileContains(t, callsLog, "snapshot:")
		assertFileContains(t, callsLog, "screenshot:")
	})
}

func assertDriverScriptShape(t *testing.T, scriptPath string) {
	t.Helper()

	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("driver script missing: %s: %v", scriptPath, err)
	}
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", scriptPath, err)
	}
	source := string(content)
	if strings.Contains(source, "odin-orchestrator-main") {
		t.Fatalf("driver script %s still references legacy repo path", scriptPath)
	}
	if !strings.Contains(source, "browser-access.sh") {
		t.Fatalf("driver script %s does not source the repo-local browser library", scriptPath)
	}
}

func browserAccessStubContent() string {
	return `#!/usr/bin/env bash
set -euo pipefail

: "${ODIN_BROWSER_STUB_CALLS_LOG:?missing calls log path}"
: "${ODIN_BROWSER_STUB_SOURCE_MARKER:?missing source marker path}"

mkdir -p "$(dirname "${ODIN_BROWSER_STUB_CALLS_LOG}")" "$(dirname "${ODIN_BROWSER_STUB_SOURCE_MARKER}")"
printf 'sourced repo-local browser-access.sh\n' > "${ODIN_BROWSER_STUB_SOURCE_MARKER}"

browser_server_health() {
    printf 'health:%s\n' "$*" >> "${ODIN_BROWSER_STUB_CALLS_LOG}"
    if [[ -n "${ODIN_BROWSER_STUB_HEALTH_EXIT_CODE:-}" ]]; then
        return "${ODIN_BROWSER_STUB_HEALTH_EXIT_CODE}"
    fi
    case "${ODIN_BROWSER_STUB_HEALTH_STATE:-healthy}" in
        healthy)
            printf '{"browser":true,"state":"healthy"}'
            ;;
        stopped)
            printf '{"browser":false,"state":"stopped"}'
            ;;
        *)
            printf '%s' "${ODIN_BROWSER_STUB_HEALTH_STATE:-healthy}"
            ;;
    esac
}

browser_request_domain_access() {
    local target="${1:-}"
    if [[ ! "${target}" =~ ^https?:// ]]; then
        printf 'browser_request_domain_access requires a full URL, got: %s\n' "${target}" >&2
        return 1
    fi
    printf 'request:%s\n' "${target}" >> "${ODIN_BROWSER_STUB_CALLS_LOG}"
    return 0
}

browser_server_start() {
    printf 'start:%s\n' "$*" >> "${ODIN_BROWSER_STUB_CALLS_LOG}"
    if [[ -n "${ODIN_BROWSER_STUB_START_EXIT_CODE:-}" ]]; then
        return "${ODIN_BROWSER_STUB_START_EXIT_CODE}"
    fi
    return 0
}

browser_server_stop() {
    printf 'stop:%s\n' "$*" >> "${ODIN_BROWSER_STUB_CALLS_LOG}"
    return 0
}

browser_snapshot() {
    printf 'snapshot:%s\n' "$*" >> "${ODIN_BROWSER_STUB_CALLS_LOG}"
    if [[ -n "${ODIN_BROWSER_STUB_SNAPSHOT_EXIT_CODE:-}" ]]; then
        return "${ODIN_BROWSER_STUB_SNAPSHOT_EXIT_CODE}"
    fi
    printf '%s' "${ODIN_BROWSER_STUB_SNAPSHOT:-Example Domain}"
}

browser_bc_screenshot() {
    local screenshot_path="${ODIN_BROWSER_STUB_SCREENSHOT_PATH:-}"
    printf 'screenshot:%s\n' "$*" >> "${ODIN_BROWSER_STUB_CALLS_LOG}"
    if [[ -n "${ODIN_BROWSER_STUB_SCREENSHOT_EXIT_CODE:-}" ]]; then
        return "${ODIN_BROWSER_STUB_SCREENSHOT_EXIT_CODE}"
    fi
    if [[ -z "${screenshot_path}" ]]; then
        screenshot_path="$(mktemp)"
        printf '%s' "${screenshot_path}"
        return 0
    fi
    mkdir -p "$(dirname "${screenshot_path}")"
    : > "${screenshot_path}"
    printf '%s' "${screenshot_path}"
}

browser_navigate() {
    printf 'navigate:%s\n' "$*" >> "${ODIN_BROWSER_STUB_CALLS_LOG}"
    if [[ -n "${ODIN_BROWSER_STUB_NAVIGATE_EXIT_CODE:-}" ]]; then
        return "${ODIN_BROWSER_STUB_NAVIGATE_EXIT_CODE}"
    fi
    return 0
}
`
}

func browserAccessScreenshotWrapperContent(repoRoot string) string {
	template := `#!/usr/bin/env bash
set -euo pipefail

: "${ODIN_BROWSER_STUB_CALLS_LOG:?missing calls log path}"
: "${ODIN_BROWSER_STUB_SOURCE_MARKER:?missing source marker path}"

mkdir -p "$(dirname "${ODIN_BROWSER_STUB_CALLS_LOG}")" "$(dirname "${ODIN_BROWSER_STUB_SOURCE_MARKER}")"
printf 'sourced repo-local browser-access.sh
' > "${ODIN_BROWSER_STUB_SOURCE_MARKER}"

source "__REPO_ROOT__/scripts/browser/browser-access.sh"

_bc_curl() {
    local method="" body="" url="" output_path=""
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -X)
                method="$2"
                shift 2
                ;;
            -H)
                shift 2
                ;;
            -d)
                body="$2"
                shift 2
                ;;
            *)
                url="$1"
                shift
                ;;
        esac
    done

    case "${url}" in
        *"/screenshot")
            output_path="$(jq -r '.path // empty' <<<"${body}")"
            if [[ -n "${output_path}" ]]; then
                mkdir -p "$(dirname "${output_path}")"
                : > "${output_path}"
            fi
            printf 'screenshot:%s
' "${body}" >> "${ODIN_BROWSER_STUB_CALLS_LOG}"
            printf '{"ok":true,"screenshot_path":"%s"}' "${output_path}"
            ;;
        *"/health")
            printf '{"browser":true,"state":"healthy"}'
            ;;
        *)
            printf 'browser helper stub received unexpected request: %s %s
' "${method}" "${url}" >&2
            return 1
            ;;
    esac
}
`
	return strings.ReplaceAll(template, "__REPO_ROOT__", repoRoot)
}

func runBrowserDriverScript(t *testing.T, repoRoot, scriptPath, scriptName, stdin string, extraEnv map[string]string) (stdout string, callsLog string, markerPath string) {
	t.Helper()

	stdout, callsLog, markerPath, err := runBrowserDriverScriptRaw(t, repoRoot, scriptPath, scriptName, stdin, extraEnv, browserAccessStubContent())
	if err != nil {
		t.Fatalf("driver %s error = %v\n%s", scriptName, err, stdout)
	}
	if stdout == "" {
		t.Fatalf("driver %s produced empty stdout", scriptName)
	}
	return stdout, callsLog, markerPath
}

func runBrowserDriverScriptRaw(t *testing.T, repoRoot, scriptPath, scriptName, stdin string, extraEnv map[string]string, browserAccessContent string) (stdout string, callsLog string, markerPath string, err error) {
	t.Helper()

	tempRoot := t.TempDir()
	driverPath := filepath.Join(tempRoot, "scripts", "drivers", scriptName)
	browserAccessPath := filepath.Join(tempRoot, "scripts", "browser", "browser-access.sh")
	callsLog = filepath.Join(tempRoot, "browser-calls.log")
	markerPath = filepath.Join(tempRoot, "browser-source-marker.txt")
	runtimeDir := filepath.Join(tempRoot, "odin")

	if err = os.MkdirAll(filepath.Dir(driverPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(driver) error = %v", err)
	}
	if err = os.MkdirAll(filepath.Dir(browserAccessPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(browser access) error = %v", err)
	}
	if err = os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(runtime) error = %v", err)
	}

	copyFile(t, scriptPath, driverPath)
	if err = os.WriteFile(browserAccessPath, []byte(browserAccessContent), 0o755); err != nil {
		t.Fatalf("WriteFile(browser access stub) error = %v", err)
	}

	cmd := exec.Command("bash", driverPath)
	cmd.Dir = repoRoot
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(),
		"ODIN_DIR="+runtimeDir,
		"ODIN_BROWSER_PORT=19227",
		"ODIN_BROWSER_STUB_CALLS_LOG="+callsLog,
		"ODIN_BROWSER_STUB_SOURCE_MARKER="+markerPath,
	)
	for key, value := range extraEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	output, execErr := cmd.CombinedOutput()
	stdout = strings.TrimSpace(string(output))
	err = execErr
	return stdout, callsLog, markerPath, err
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()

	content, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", src, err)
	}
	if err := os.WriteFile(dst, content, 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", dst, err)
	}
}

func assertStructuredDriverOutput(t *testing.T, stdout, wantToolKey, wantStatus string) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("driver output is not valid json: %v\nstdout=%s", err, stdout)
	}
	if got := stringValue(payload["status"]); got != wantStatus {
		t.Fatalf("status = %q, want %q", got, wantStatus)
	}
	if got := stringValue(payload["tool_key"]); got != wantToolKey {
		t.Fatalf("tool_key = %q, want %q", got, wantToolKey)
	}
	artifacts, ok := payload["artifacts"].(map[string]any)
	if !ok || len(artifacts) == 0 {
		t.Fatalf("artifacts = %#v, want non-empty object", payload["artifacts"])
	}
}

func assertJSONArtifactString(t *testing.T, stdout, key, want string) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("driver output is not valid json: %v\nstdout=%s", err, stdout)
	}
	artifacts, ok := payload["artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("artifacts = %#v, want object", payload["artifacts"])
	}
	if got := stringValue(artifacts[key]); got != want {
		t.Fatalf("artifacts[%s] = %q, want %q", key, got, want)
	}
}

func assertFileContains(t *testing.T, path, needle string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if !strings.Contains(string(content), needle) {
		t.Fatalf("%s does not contain %q", path, needle)
	}
}

func assertFileNotContains(t *testing.T, path, needle string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if strings.Contains(string(content), needle) {
		t.Fatalf("%s unexpectedly contains %q", path, needle)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing: %s: %v", path, err)
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}
