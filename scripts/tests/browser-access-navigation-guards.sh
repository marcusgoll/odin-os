#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ACCESS_SH="${ROOT_DIR}/scripts/browser/browser-access.sh"

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

pass() {
    echo "PASS: $*"
}

[[ -f "${ACCESS_SH}" ]] || fail "missing browser-access.sh"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

export ODIN_DIR="${WORK_DIR}/odin-browser"
export ODIN_BROWSER_DOMAIN_DENYLIST="blocked.example,*.deny.test"

source "${ACCESS_SH}"

curl_calls=0
node_calls=0
_bc_curl() {
    curl_calls=$((curl_calls + 1))
    return 99
}
node() {
    node_calls=$((node_calls + 1))
    return 99
}

if browser_navigate "https://blocked.example/path"; then
    fail "expected browser_navigate to reject blocked.example"
fi
[[ "${curl_calls}" -eq 0 ]] || fail "browser_navigate called through to the local server"

if browser_server_start --url "https://blocked.example/path"; then
    fail "expected browser_server_start to reject blocked.example before launch"
fi
[[ "${node_calls}" -eq 0 ]] || fail "browser_server_start launched the runtime for a blocked URL"
[[ "${curl_calls}" -eq 0 ]] || fail "blocked browser_server_start called through to the local server"

pass "blocked navigation paths stop before the local browser server"
