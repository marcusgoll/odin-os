#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BROWSER_DIR="${ROOT_DIR}/scripts/browser"
ACCESS_SH="${BROWSER_DIR}/browser-access.sh"
SERVER_JS="${BROWSER_DIR}/odin-huginn-server.js"
CAPTCHA_JS="${BROWSER_DIR}/huginn-captcha.js"

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

pass() {
    echo "PASS: $*"
}

for path in "${ACCESS_SH}" "${SERVER_JS}" "${CAPTCHA_JS}"; do
    [[ -f "${path}" ]] || fail "missing repo-local browser runtime file: ${path}"
done
pass "repo-local browser runtime files exist"

ODIN_DIR="${ROOT_DIR}/.odin-browser-smoke"
export ODIN_DIR
export ODIN_BROWSER_PORT="${ODIN_BROWSER_PORT:-19227}"
mkdir -p "${ODIN_DIR}"

source "${ACCESS_SH}"

cleanup() {
    browser_server_stop >/dev/null 2>&1 || true
}
trap cleanup EXIT

if ! browser_server_start --headless; then
    fail "browser_server_start could not launch Chromium"
fi
pass "browser_server_start launched Chromium"

HEALTH_JSON="$(curl -sf "${BROWSER_SERVER_URL}/health")"
ENGINE="$(echo "${HEALTH_JSON}" | jq -r '.engine // empty')"
[[ "${ENGINE}" == "chromium" ]] || fail "expected chromium engine, got '${ENGINE:-empty}'"
pass "browser health reports chromium"

LOCAL_URL='data:text/html,<title>BrowserSmokeLocal</title><body>BrowserSmokeLocal</body>'
if ! browser_navigate "${LOCAL_URL}"; then
    fail "browser_navigate could not load local data URL"
fi
LOCAL_SNAPSHOT="$(browser_snapshot)"
[[ "${LOCAL_SNAPSHOT}" == *"BrowserSmokeLocal"* ]] || fail "browser_snapshot did not return local data URL content"
pass "browser_snapshot returned local data URL content"

if ! browser_navigate "https://example.com"; then
    fail "browser_navigate could not load example.com"
fi
SNAPSHOT="$(browser_snapshot)"
[[ "${SNAPSHOT}" == *"Example Domain"* ]] || fail "browser_snapshot did not return Example Domain"
pass "browser_snapshot returned Example Domain"

browser_server_stop >/dev/null
trap - EXIT
pass "browser_server_stop completed"
