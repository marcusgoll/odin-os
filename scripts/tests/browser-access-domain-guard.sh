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
if browser_request_domain_access "https://blocked.example./path"; then
    fail "expected blocked.example. to be denied"
fi
if browser_request_domain_access "https://blocked.example%2e/path"; then
    fail "expected blocked.example%2e to be denied"
fi
if browser_request_domain_access "example.com/path"; then
    fail "expected scheme-less example.com/path to be denied"
fi
if browser_request_domain_access "//example.com/path"; then
    fail "expected scheme-relative //example.com/path to be denied"
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
for target in \
    'javascript:alert(1)' \
    'chrome://settings/' \
    'file:///tmp/browser-access-domain-guard' \
    'chrome-extension://abcdef/popup.html' \
    'devtools://devtools/bundled/inspector.html' \
    'view-source:https://example.com'; do
    if browser_request_domain_access "${target}"; then
        fail "expected ${target} to be denied"
    fi
done
for target in \
    'mailto:user@example.com' \
    'ftp://example.com/resource' \
    'custom-scheme://example.com/path'; do
    if browser_request_domain_access "${target}"; then
        fail "expected ${target} to be denied by scheme allowlist"
    fi
done
if browser_request_domain_access "https://localhost./path"; then
    fail "expected localhost. to be denied"
fi
if browser_request_domain_access "https://foo.localhost/path"; then
    fail "expected foo.localhost to be denied"
fi
if browser_request_domain_access "https://127.0.0.2/path"; then
    fail "expected 127.0.0.2 to be denied"
fi
if browser_request_domain_access "https://127.0.0.1%2e/path"; then
    fail "expected 127.0.0.1%2e to be denied"
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
for target in \
    'https://10.0.0.1/path' \
    'https://172.16.0.1/path' \
    'https://192.168.0.1/path' \
    'https://[fd00::1]/path' \
    'https://[fe80::1%25lo]/path'; do
    if browser_request_domain_access "${target}"; then
        fail "expected ${target} to be denied as a local-service target"
    fi
done
browser_request_domain_access "data:text/html,allowed" || fail "expected data: URLs to remain allowed"
if browser_request_domain_access "https://[::ffff:7f00:1]/path"; then
    fail "expected canonical IPv4-mapped IPv6 loopback to be denied"
fi
pass "domain denylist blocks matches and denies canonical local-service forms"
