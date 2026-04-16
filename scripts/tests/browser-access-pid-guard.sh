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

export BA_PROC_ROOT="${WORK_DIR}/proc"
export ODIN_DIR="${WORK_DIR}/odin-browser"
export ODIN_BROWSER_PORT="19227"

mkdir -p "${BA_PROC_ROOT}/4242" "${BA_PROC_ROOT}/4243" "${ODIN_DIR}/browser-state"

source "${ACCESS_SH}"

printf 'node\0%s\0' "${BROWSER_SERVER_SCRIPT}" > "${BA_PROC_ROOT}/4242/cmdline"
printf 'ODIN_DIR=%s\0ODIN_BROWSER_PORT=%s\0' "${ODIN_DIR}" "${ODIN_BROWSER_PORT}" > "${BA_PROC_ROOT}/4242/environ"

printf 'node\0/elsewhere.js\0' > "${BA_PROC_ROOT}/4243/cmdline"
printf 'ODIN_DIR=%s\0ODIN_BROWSER_PORT=%s\0' "${ODIN_DIR}" "${ODIN_BROWSER_PORT}" > "${BA_PROC_ROOT}/4243/environ"

_ba_pid_is_browser_runtime "4242" || fail "expected runtime PID to be recognized"
if _ba_pid_is_browser_runtime "4243"; then
    fail "expected unrelated PID to be rejected"
fi

pass "PID ownership guard distinguishes browser runtime from unrelated processes"
