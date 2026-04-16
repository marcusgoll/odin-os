#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SERVER_JS="${ROOT_DIR}/scripts/browser/odin-huginn-server.js"

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

pass() {
    echo "PASS: $*"
}

[[ -f "${SERVER_JS}" ]] || fail "missing odin-huginn-server.js"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

export ODIN_DIR="${WORK_DIR}/odin-browser"
export ODIN_BROWSER_DOMAIN_DENYLIST="blocked.example,*.deny.test"
export ODIN_BROWSER_PORT="$(node - <<'NODE'
const net = require('node:net');
const server = net.createServer();
server.on('error', (error) => {
  console.error(error.message);
  process.exit(1);
});
server.listen(0, '127.0.0.1', () => {
  const address = server.address();
  const port = address && typeof address === 'object' ? address.port : null;
  server.close(() => {
    if (!port) process.exit(1);
    console.log(port);
  });
});
NODE
)"

mkdir -p "${ODIN_DIR}"

SERVER_LOG="${WORK_DIR}/server.log"
node "${SERVER_JS}" >"${SERVER_LOG}" 2>&1 &
SERVER_PID=$!

cleanup() {
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" >/dev/null 2>&1 || true
    rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

BROWSER_SERVER_URL="http://127.0.0.1:${ODIN_BROWSER_PORT}"

for _ in $(seq 1 100); do
    if curl -sf "${BROWSER_SERVER_URL}/health" >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done
curl -sf "${BROWSER_SERVER_URL}/health" >/dev/null || fail "server did not become healthy"
pass "direct server started"

assert_rejected() {
    local endpoint="${1:-}" target="${2:-}" status
    status="$(curl -s -o /dev/null -w '%{http_code}' -X POST "${BROWSER_SERVER_URL}/${endpoint}" -H 'Content-Type: application/json' -d "$(jq -nc --arg url "${target}" '{url: $url, browser: "chromium", headless: true}')")"
    [[ "${status}" != "200" ]] || fail "expected ${endpoint} to reject ${target}"
    [[ "$(curl -sf "${BROWSER_SERVER_URL}/health" | jq -r '.browser')" == "false" ]] || fail "${endpoint} unexpectedly launched Chromium for ${target}"
}

for target in \
    'https://blocked.example/path' \
    'https://blocked.example%2e/path' \
    'https://foo.deny.test/resource'; do
    assert_rejected launch "${target}"
    assert_rejected navigate "${target}"
done
pass "direct server denies denylisted domains"

for target in \
    'javascript:alert(1)' \
    'chrome://settings/' \
    'file:///tmp/browser-access-direct-server-guards' \
    'chrome-extension://abcdef/popup.html' \
    'devtools://devtools/bundled/inspector.html' \
    'view-source:https://example.com'; do
    assert_rejected launch "${target}"
    assert_rejected navigate "${target}"
done
pass "direct server blocks the expanded scheme set"
