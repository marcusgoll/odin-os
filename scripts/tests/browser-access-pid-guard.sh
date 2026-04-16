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

mkdir -p "${BA_PROC_ROOT}/4242" "${BA_PROC_ROOT}/4243" "${BA_PROC_ROOT}/4244" "${ODIN_DIR}/browser-state"

source "${ACCESS_SH}"

OLD_PORT="24444"
printf 'node\0%s\0' "${BROWSER_SERVER_SCRIPT}" > "${BA_PROC_ROOT}/4242/cmdline"
printf 'ODIN_DIR=%s\0ODIN_BROWSER_PORT=%s\0' "${ODIN_DIR}" "${OLD_PORT}" > "${BA_PROC_ROOT}/4242/environ"

printf 'node\0%s.bak\0' "${BROWSER_SERVER_SCRIPT}" > "${BA_PROC_ROOT}/4243/cmdline"
printf 'ODIN_DIR=%s\0ODIN_BROWSER_PORT=%s\0' "${ODIN_DIR}" "${OLD_PORT}" > "${BA_PROC_ROOT}/4243/environ"

printf 'node\0/elsewhere.js\0' > "${BA_PROC_ROOT}/4244/cmdline"
printf 'ODIN_DIR=%s\0ODIN_BROWSER_PORT=%s\0' "${ODIN_DIR}" "${OLD_PORT}" > "${BA_PROC_ROOT}/4244/environ"

printf '4242\n' > "${BROWSER_SERVER_PID_FILE}"
printf '%s\n' "${OLD_PORT}" > "${BROWSER_SERVER_PORT_FILE}"

kill_calls=()
kill() {
    kill_calls+=("$*")
    return 0
}

curl_calls=()
_bc_curl() {
    curl_calls+=("$*")
    return 0
}

browser_server_stop

[[ "${#curl_calls[@]}" -eq 1 ]] || fail "expected one stop call, saw ${#curl_calls[@]}"
[[ "${curl_calls[0]}" == *"http://127.0.0.1:${OLD_PORT}/stop"* ]] || fail "browser_server_stop did not target the persisted runtime port"
[[ "${#kill_calls[@]}" -ge 1 ]] || fail "expected kill to be used for stale runtime cleanup"
[[ "${kill_calls[0]}" == "4242" ]] || fail "expected browser_server_stop to try terminating the runtime pid"
[[ ! -f "${BROWSER_SERVER_PID_FILE}" ]] || fail "browser pid file was not cleared"
[[ ! -f "${BROWSER_SERVER_PORT_FILE}" ]] || fail "browser port file was not cleared"

_ba_pid_is_browser_runtime "4242" "${OLD_PORT}" || fail "expected runtime PID to be recognized with the persisted port"
if _ba_pid_is_browser_runtime "4243" "${OLD_PORT}"; then
    fail "expected path-fragment PID to be rejected"
fi
if _ba_pid_is_browser_runtime "4244" "${OLD_PORT}"; then
    fail "expected unrelated PID to be rejected"
fi

pass "PID ownership and persisted-port stop path distinguish browser runtime from false positives"
