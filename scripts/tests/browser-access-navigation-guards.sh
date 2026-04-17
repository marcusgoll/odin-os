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
if browser_navigate "https://blocked.example%2e/path"; then
    fail "expected browser_navigate to reject blocked.example%2e"
fi
if browser_navigate "example.com/path"; then
    fail "expected browser_navigate to reject scheme-less example.com/path"
fi
if browser_server_start --url "//example.com/path"; then
    fail "expected browser_server_start to reject scheme-relative //example.com/path before launch"
fi
[[ "${curl_calls}" -eq 0 ]] || fail "browser_navigate called through to the local server"

if browser_server_start --url "https://blocked.example/path"; then
    fail "expected browser_server_start to reject blocked.example before launch"
fi
[[ "${node_calls}" -eq 0 ]] || fail "browser_server_start launched the runtime for a blocked URL"
if browser_navigate "http://localhost./path"; then
    fail "expected browser_navigate to reject localhost."
fi
if browser_server_start --url "https://127.0.0.1%2e/path"; then
    fail "expected browser_server_start to reject 127.0.0.1%2e before launch"
fi
for target in \
    'javascript:alert(1)' \
    'chrome://settings/' \
    'file:///tmp/browser-access-navigation-guards' \
    'chrome-extension://abcdef/popup.html' \
    'devtools://devtools/bundled/inspector.html' \
    'view-source:https://example.com'; do
    if browser_server_start --url "${target}"; then
        fail "expected browser_server_start to reject ${target} before launch"
    fi
done
for target in \
    'mailto:user@example.com' \
    'ftp://example.com/resource' \
    'custom-scheme://example.com/path'; do
    if browser_navigate "${target}"; then
        fail "expected browser_navigate to reject ${target}"
    fi
    if browser_server_start --url "${target}"; then
        fail "expected browser_server_start to reject ${target} before launch"
    fi
done
for target in \
    'https://10.0.0.1/path' \
    'https://172.16.0.1/path' \
    'https://192.168.0.1/path' \
    'https://[fd00::1]/path' \
    'https://[fe80::1%25lo]/path'; do
    if browser_navigate "${target}"; then
        fail "expected browser_navigate to reject ${target}"
    fi
    if browser_server_start --url "${target}"; then
        fail "expected browser_server_start to reject ${target} before launch"
    fi
done
[[ "${curl_calls}" -eq 0 ]] || fail "blocked browser_server_start called through to the local server"

pass "blocked navigation paths stop before the local browser server"
