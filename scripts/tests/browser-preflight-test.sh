#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PREFLIGHT_SH="${ROOT_DIR}/scripts/ops/browser-preflight.sh"
TMP_BIN_DIR=""
HOST_CHROME_BIN=""
HOST_CHROME_RUNTIME=""

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

pass() {
    echo "PASS: $*"
}

cleanup() {
    [[ -n "${TMP_BIN_DIR}" && -d "${TMP_BIN_DIR}" ]] && rm -rf "${TMP_BIN_DIR}"
}

trap cleanup EXIT

setup_chrome_path() {
    TMP_BIN_DIR="$(mktemp -d)"
    local tool target candidate

    for tool in bash readlink dirname basename grep ldd; do
        target="$(command -v "${tool}")"
        [[ -n "${target}" ]] || fail "missing required host utility: ${tool}"
        ln -s "${target}" "${TMP_BIN_DIR}/${tool}"
    done

    for candidate in google-chrome chrome chromium chromium-browser; do
        target="$(command -v "${candidate}" 2>/dev/null || true)"
        if [[ -n "${target}" ]]; then
            HOST_CHROME_BIN="${target}"
            break
        fi
    done

    [[ -n "${HOST_CHROME_BIN}" ]] || fail "missing required host chromium launcher"

    HOST_CHROME_RUNTIME="$(readlink -f "${HOST_CHROME_BIN}" 2>/dev/null || true)"
    [[ -n "${HOST_CHROME_RUNTIME}" ]] || fail "unable to resolve host chromium runtime for ${HOST_CHROME_BIN}"
    [[ -x "${HOST_CHROME_RUNTIME}" ]] || fail "resolved host chromium runtime is not executable: ${HOST_CHROME_RUNTIME}"

    ln -s "${HOST_CHROME_BIN}" "${TMP_BIN_DIR}/chrome"
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

assert_success_contains() {
    local description="$1"
    local expected="$2"
    shift 2
    local status output result
    result="$(run_preflight "$@")"
    status="${result%%$'\n'*}"
    output="${result#*$'\n'}"
    [[ "${status}" -eq 0 ]] || fail "${description}: expected success, got ${status}; output: ${output}"
    [[ "${output}" == *"${expected}"* ]] || fail "${description}: missing expected text '${expected}'; output: ${output}"
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

setup_chrome_path

assert_success_contains \
    "chromium preflight passes with bare chrome on PATH" \
    "wrapper=${TMP_BIN_DIR}/chrome" \
    env PATH="${TMP_BIN_DIR}" bash "${PREFLIGHT_SH}"

assert_success_contains \
    "chromium preflight passes against the host launcher chain" \
    "wrapper=${HOST_CHROME_BIN}" \
    bash "${PREFLIGHT_SH}"

assert_failure_contains \
    "missing chromium binary fails closed" \
    "Chromium browser binary is missing" \
    env BROWSER_PREFLIGHT_CHROME_BIN="/does/not/exist/google-chrome" bash "${PREFLIGHT_SH}"

assert_failure_contains \
    "missing runtime libraries fails closed" \
    "required runtime libraries are missing" \
    env PATH="${TMP_BIN_DIR}" BROWSER_PREFLIGHT_LDD_OUTPUT=$'linux-vdso.so.1 (0x00007ffd)\nlibmissing.so.1 => not found\n' bash "${PREFLIGHT_SH}"

assert_failure_contains \
    "firefox is rejected in this phase" \
    "Chromium only" \
    env PATH="${TMP_BIN_DIR}" bash "${PREFLIGHT_SH}" --engine firefox

assert_failure_contains \
    "webkit is rejected in this phase" \
    "Chromium only" \
    env PATH="${TMP_BIN_DIR}" bash "${PREFLIGHT_SH}" --engine webkit

pass "browser preflight contract verified"
