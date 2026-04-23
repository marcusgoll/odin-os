package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserAccessReusesConfiguredServerURL(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "browser", "browser-access.sh")

	tempRoot := t.TempDir()
	runtimeRoot := filepath.Join(tempRoot, "odin")
	callsLog := filepath.Join(tempRoot, "browser-calls.log")
	probePath := filepath.Join(tempRoot, "probe.sh")

	probe := strings.ReplaceAll(`#!/usr/bin/env bash
set -euo pipefail

source "__SCRIPT_PATH__"

_bc_curl() {
    local method="GET" url=""
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
                shift 2
                ;;
            *)
                url="$1"
                shift
                ;;
        esac
    done

    printf 'curl:%s %s\n' "${method}" "${url}" >> "__CALLS_LOG__"
    case "${url}" in
        "http://remote-browser.test:7777/health")
            printf '{"browser":true,"engine":"chromium","state":"healthy"}'
            ;;
        "http://remote-browser.test:7777/navigate")
            printf '{"ok":true}'
            ;;
        "http://remote-browser.test:7777/snapshot")
            printf '{"snapshot":"Review your transfer"}'
            ;;
        *)
            return 1
            ;;
    esac
}

browser_server_start --url "https://robinhood.com/transfer" --headed
browser_navigate "https://robinhood.com/transfer"
snapshot="$(browser_snapshot)"
printf 'snapshot=%s\n' "${snapshot}"
browser_server_stop
`, "__SCRIPT_PATH__", scriptPath)
	probe = strings.ReplaceAll(probe, "__CALLS_LOG__", callsLog)

	if err := os.WriteFile(probePath, []byte(probe), 0o755); err != nil {
		t.Fatalf("WriteFile(probe) error = %v", err)
	}

	cmd := exec.Command("timeout", "5", "bash", probePath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ODIN_DIR="+runtimeRoot,
		"ODIN_BROWSER_PORT=19227",
		"ODIN_BROWSER_SERVER_URL=http://remote-browser.test:7777",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser access probe failed: %v\n%s", err, string(output))
	}

	if !strings.Contains(string(output), "snapshot=Review your transfer") {
		t.Fatalf("probe output = %q, want snapshot output", string(output))
	}
	assertFileContainsSubstring(t, callsLog, "curl:GET http://remote-browser.test:7777/health")
	assertFileContainsSubstring(t, callsLog, "curl:POST http://remote-browser.test:7777/navigate")
	assertFileContainsSubstring(t, callsLog, "curl:GET http://remote-browser.test:7777/snapshot")
	assertFileNotContains(t, callsLog, "127.0.0.1")
}
