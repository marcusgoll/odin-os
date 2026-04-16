#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PREFLIGHT_SH="${ROOT_DIR}/scripts/ops/browser-preflight.sh"

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

pass() {
    echo "PASS: $*"
}

run_preflight() {
    local output status
    if output="$("$@" 2>&1)"; then
        status=0
    else
        status=$?
    fi
    printf '%s\n%s' "${status}" "${output}"
}

assert_success() {
    local description="$1"
    shift
    local status output result
    result="$(run_preflight "$@")"
    status="${result%%$'\n'*}"
    output="${result#*$'\n'}"
    [[ "${status}" -eq 0 ]] || fail "${description}: expected success, got ${status}; output: ${output}"
    [[ "${output}" == *"READY:"* ]] || fail "${description}: missing readiness summary; output: ${output}"
    [[ "${output}" == *"Chromium"* ]] || fail "${description}: readiness summary did not mention Chromium; output: ${output}"
    pass "${description}"
}

assert_failure_contains() {
    local description="$1"
    local expected="$2"
    shift 2
    local status output result
    result="$(run_preflight "$@")"
    status="${result%%$'\n'*}"
    output="${result#*$'\n'}"
    [[ "${status}" -ne 0 ]] || fail "${description}: expected failure; output: ${output}"
    [[ "${output}" == *"${expected}"* ]] || fail "${description}: missing expected text '${expected}'; output: ${output}"
    pass "${description}"
}

if [[ ! -f "${PREFLIGHT_SH}" ]]; then
    fail "missing preflight script: ${PREFLIGHT_SH}"
fi

CHROME_BIN="/usr/bin/google-chrome"
[[ -x "${CHROME_BIN}" ]] || fail "expected host Chromium binary at ${CHROME_BIN}"

assert_success \
    "chromium preflight passes on the live host" \
    env BROWSER_PREFLIGHT_CHROME_BIN="${CHROME_BIN}" bash "${PREFLIGHT_SH}"

assert_failure_contains \
    "missing chromium binary fails closed" \
    "Chromium browser binary is missing" \
    env BROWSER_PREFLIGHT_CHROME_BIN="/does/not/exist/google-chrome" bash "${PREFLIGHT_SH}"

assert_failure_contains \
    "missing runtime libraries fails closed" \
    "required runtime libraries are missing" \
    env BROWSER_PREFLIGHT_CHROME_BIN="${CHROME_BIN}" BROWSER_PREFLIGHT_LDD_OUTPUT=$'linux-vdso.so.1 (0x00007ffd)\nlibmissing.so.1 => not found\n' bash "${PREFLIGHT_SH}"

assert_failure_contains \
    "firefox is rejected in this phase" \
    "Chromium only" \
    env BROWSER_PREFLIGHT_CHROME_BIN="${CHROME_BIN}" bash "${PREFLIGHT_SH}" --engine firefox

assert_failure_contains \
    "webkit is rejected in this phase" \
    "Chromium only" \
    env BROWSER_PREFLIGHT_CHROME_BIN="${CHROME_BIN}" bash "${PREFLIGHT_SH}" --engine webkit

pass "browser preflight contract verified"
