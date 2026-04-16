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
if browser_request_domain_access "https://user:pass@blocked.example/path"; then
    fail "expected credentialed blocked.example to be denied"
fi
if browser_request_domain_access "https://foo.deny.test/resource"; then
    fail "expected foo.deny.test to be denied by wildcard"
fi
if browser_request_domain_access "https://127.1/path"; then
    fail "expected 127.1 to be denied as a loopback alias"
fi
if browser_request_domain_access "https://0x7f000001/path"; then
    fail "expected 0x7f000001 to be denied as a loopback alias"
fi
if browser_request_domain_access "https://017700000001/path"; then
    fail "expected 017700000001 to be denied as a loopback alias"
fi
browser_request_domain_access "https://example.com" || fail "expected example.com to be allowed"
if browser_request_domain_access "javascript:alert(1)"; then
    fail "expected javascript: URLs to be denied"
fi
if browser_request_domain_access "chrome://settings/"; then
    fail "expected chrome: URLs to be denied"
fi
if browser_request_domain_access "https://localhost./path"; then
    fail "expected localhost. to be denied"
fi
if browser_request_domain_access "https://foo.localhost/path"; then
    fail "expected foo.localhost to be denied"
fi
if browser_request_domain_access "https://127.0.0.2/path"; then
    fail "expected 127.0.0.2 to be denied"
fi
if browser_request_domain_access "https://2130706433/path"; then
    fail "expected integer localhost to be denied"
fi
if browser_request_domain_access "https://[::ffff:127.0.0.1]/path"; then
    fail "expected IPv4-mapped IPv6 loopback to be denied"
fi
if browser_request_domain_access "https://[::1%25lo]/path"; then
    fail "expected IPv6 zone-id loopback to be denied"
fi
if browser_request_domain_access "https://[::ffff:127.0.0.1%25lo]/path"; then
    fail "expected IPv4-mapped IPv6 zone-id loopback to be denied"
fi
browser_request_domain_access "data:text/html,allowed" || fail "expected data: URLs to remain allowed"
if browser_request_domain_access "https://[::ffff:7f00:1]/path"; then
    fail "expected canonical IPv4-mapped IPv6 loopback to be denied"
fi
pass "domain denylist blocks matches and denies canonical local-service forms"
