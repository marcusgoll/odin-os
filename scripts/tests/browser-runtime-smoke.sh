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

WORK_DIR="$(mktemp -d)"
ODIN_DIR="${WORK_DIR}/odin-browser-smoke"
export ODIN_DIR
unset ODIN_BROWSER_PORT
mkdir -p "${ODIN_DIR}"

PORT_HOLDER_PID=""
node -e 'const net = require("node:net"); const server = net.createServer(); server.on("error", (error) => { console.error(error.message); process.exit(1); }); server.listen(19227, "127.0.0.1", () => { setInterval(() => {}, 1 << 30); });' >/dev/null 2>&1 &
PORT_HOLDER_PID=$!

source "${ACCESS_SH}"

cleanup() {
    browser_server_stop >/dev/null 2>&1 || true
    if [[ -n "${PORT_HOLDER_PID}" ]]; then
        kill "${PORT_HOLDER_PID}" >/dev/null 2>&1 || true
        wait "${PORT_HOLDER_PID}" >/dev/null 2>&1 || true
    fi
    rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

[[ -n "${BROWSER_SERVER_PORT}" ]] || fail "browser_server_port was not resolved"
[[ "${BROWSER_SERVER_PORT}" != "19227" ]] || fail "browser_server_port unexpectedly used the fixed default"
[[ "${BROWSER_SERVER_URL}" == "http://127.0.0.1:${BROWSER_SERVER_PORT}" ]] || fail "browser_server_url did not match the resolved port"
pass "browser_server_port resolved dynamically"

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
rm -rf "${WORK_DIR}"
pass "browser_server_stop completed"
