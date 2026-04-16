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

until node -e 'const net = require("node:net"); const socket = net.createConnection({ host: "127.0.0.1", port: 19227 }); socket.on("connect", () => { socket.end(); process.exit(0); }); socket.on("error", () => process.exit(1));' >/dev/null 2>&1; do
    sleep 0.1
done

source "${ACCESS_SH}"

INITIAL_BROWSER_SERVER_PORT="${BROWSER_SERVER_PORT}"
RETRY_PORT_HOLDER_PID=""
node -e 'const net = require("node:net"); const port = Number(process.argv[1]); const server = net.createServer(); server.on("error", (error) => { console.error(error.message); process.exit(1); }); server.listen(port, "127.0.0.1", () => { setInterval(() => {}, 1 << 30); });' "${INITIAL_BROWSER_SERVER_PORT}" >/dev/null 2>&1 &
RETRY_PORT_HOLDER_PID=$!

wait_for_port() {
    local port="${1:-}" attempt=0
    while ! node -e 'const net = require("node:net"); const port = Number(process.argv[1]); const socket = net.createConnection({ host: "127.0.0.1", port }); socket.on("connect", () => { socket.end(); process.exit(0); }); socket.on("error", () => process.exit(1));' "${port}" >/dev/null 2>&1; do
        attempt=$((attempt + 1))
        [[ "${attempt}" -gt 50 ]] && return 1
        sleep 0.1
    done
}

wait_for_port "${INITIAL_BROWSER_SERVER_PORT}"

cleanup() {
    browser_server_stop >/dev/null 2>&1 || true
    if [[ -n "${RETRY_PORT_HOLDER_PID}" ]]; then
        kill "${RETRY_PORT_HOLDER_PID}" >/dev/null 2>&1 || true
        wait "${RETRY_PORT_HOLDER_PID}" >/dev/null 2>&1 || true
    fi
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
[[ "${BROWSER_SERVER_PORT}" != "${INITIAL_BROWSER_SERVER_PORT}" ]] || fail "browser_server_start did not retry on a new port"
[[ "${BROWSER_SERVER_URL}" == "http://127.0.0.1:${BROWSER_SERVER_PORT}" ]] || fail "browser_server_url did not update after retry"
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
CURRENT_URL="$(curl -sf "${BROWSER_SERVER_URL}/health" | jq -r '.url // empty')"
[[ "${CURRENT_URL}" == "https://example.com/" ]] || fail "browser health did not report the committed example.com URL"
SNAPSHOT="$(browser_snapshot)"
[[ "${SNAPSHOT}" == *"Example Domain"* ]] || fail "browser_snapshot did not return Example Domain"
pass "browser_snapshot returned Example Domain"

for target in     'http://localhost/path'     'http://127.0.0.2/path'     'https://[::1]/path'     'https://[::ffff:127.0.0.1]/path'; do
    NAVIGATE_STATUS="$(curl -s -o /dev/null -w '%{http_code}' -X POST "${BROWSER_SERVER_URL}/navigate" -H 'Content-Type: application/json' -d "$(jq -nc --arg url "${target}" '{url: $url}')")"
    [[ "${NAVIGATE_STATUS}" != "200" ]] || fail "direct navigate unexpectedly succeeded for ${target}"
    CURRENT_URL_AFTER_NAVIGATE="$(curl -sf "${BROWSER_SERVER_URL}/health" | jq -r '.url // empty')"
    [[ "${CURRENT_URL_AFTER_NAVIGATE}" == "https://example.com/" ]] || fail "direct navigate changed the current URL for ${target}"
done
pass "direct navigate rejects local-service URLs"

for target in     'http://localhost/path'     'http://127.0.0.2/path'     'https://[::1]/path'     'https://[::ffff:127.0.0.1]/path'; do
    LAUNCH_STATUS="$(curl -s -o /dev/null -w '%{http_code}' -X POST "${BROWSER_SERVER_URL}/launch" -H 'Content-Type: application/json' -d "$(jq -nc --arg url "${target}" '{browser:"chromium", headless: true, url: $url}')")"
    [[ "${LAUNCH_STATUS}" != "200" ]] || fail "direct launch unexpectedly succeeded for ${target}"
    CURRENT_URL_AFTER_LAUNCH="$(curl -sf "${BROWSER_SERVER_URL}/health" | jq -r '.url // empty')"
    [[ "${CURRENT_URL_AFTER_LAUNCH}" == "https://example.com/" ]] || fail "direct launch changed the current URL for ${target}"
done
pass "direct launch rejects local-service URLs"

browser_server_stop >/dev/null
trap - EXIT
rm -rf "${WORK_DIR}"
pass "browser_server_stop completed"
