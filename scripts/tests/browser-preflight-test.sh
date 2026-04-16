#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PREFLIGHT_SH="${ROOT_DIR}/scripts/ops/browser-preflight.sh"
TMP_BIN_DIR=""

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

setup_stub_path() {
    TMP_BIN_DIR="$(mktemp -d)"
    local tool target

    for tool in bash readlink dirname basename grep ldd; do
        target="$(command -v "${tool}")"
        [[ -n "${target}" ]] || fail "missing required host utility: ${tool}"
        ln -s "${target}" "${TMP_BIN_DIR}/${tool}"
    done

    cat >"${TMP_BIN_DIR}/chrome" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
    chmod +x "${TMP_BIN_DIR}/chrome"
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

setup_stub_path
GOOD_LDD_OUTPUT=$'linux-vdso.so.1 (0x00007ffd)\n'

assert_success \
    "chromium preflight passes with bare chrome on PATH" \
    env PATH="${TMP_BIN_DIR}" BROWSER_PREFLIGHT_LDD_OUTPUT="${GOOD_LDD_OUTPUT}" bash "${PREFLIGHT_SH}"

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
    env PATH="${TMP_BIN_DIR}" BROWSER_PREFLIGHT_LDD_OUTPUT="${GOOD_LDD_OUTPUT}" bash "${PREFLIGHT_SH}" --engine firefox

assert_failure_contains \
    "webkit is rejected in this phase" \
    "Chromium only" \
    env PATH="${TMP_BIN_DIR}" BROWSER_PREFLIGHT_LDD_OUTPUT="${GOOD_LDD_OUTPUT}" bash "${PREFLIGHT_SH}" --engine webkit

pass "browser preflight contract verified"
