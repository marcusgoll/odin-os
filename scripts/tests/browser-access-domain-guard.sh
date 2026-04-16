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

if browser_request_domain_access "https://blocked.example/path"; then
    fail "expected blocked.example to be denied"
fi
if browser_request_domain_access "https://foo.deny.test/resource"; then
    fail "expected foo.deny.test to be denied by wildcard"
fi
browser_request_domain_access "https://example.com" || fail "expected example.com to be allowed"
pass "domain denylist blocks matches and allows non-blocked domains"
