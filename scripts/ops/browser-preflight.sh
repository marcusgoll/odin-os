#!/usr/bin/env bash
set -euo pipefail

ENGINE="chromium"
CHROME_CANDIDATE=""

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

usage() {
    cat <<'USAGE'
Usage: browser-preflight.sh [--engine chromium]

Chromium-only browser readiness check for the odin-os browser cutover.
USAGE
}

resolve_chromium_candidate() {
    local candidate=""
    if [[ -n "${BROWSER_PREFLIGHT_CHROME_BIN:-}" ]]; then
        candidate="${BROWSER_PREFLIGHT_CHROME_BIN}"
    elif [[ -n "${CHROME_BIN:-}" ]]; then
        candidate="${CHROME_BIN}"
    else
        for candidate in google-chrome google-chrome-stable chromium chromium-browser chrome; do
            if command -v "${candidate}" >/dev/null 2>&1; then
                command -v "${candidate}"
                return 0
            fi
        done
        return 1
    fi

    if [[ "${candidate}" == */* ]]; then
        [[ -x "${candidate}" ]] || return 1
        printf '%s\n' "${candidate}"
        return 0
    fi

    if command -v "${candidate}" >/dev/null 2>&1; then
        command -v "${candidate}"
        return 0
    fi

    return 1
}

resolve_chromium_runtime() {
    local candidate="${1:-}" resolved dir base runtime
    [[ -n "${candidate}" ]] || return 1

    resolved="$(readlink -f "${candidate}" 2>/dev/null || printf '%s' "${candidate}")"
    dir="$(dirname "${resolved}")"
    base="$(basename "${resolved}")"

    case "${base}" in
        google-chrome|google-chrome-stable|chromium-browser|chrome)
            runtime="${dir}/chrome"
            if [[ -x "${runtime}" ]]; then
                printf '%s\n' "${runtime}"
                return 0
            fi
            ;;
    esac

    if [[ -x "${resolved}" ]]; then
        printf '%s\n' "${resolved}"
        return 0
    fi

    return 1
}

check_supported_engine() {
    case "${ENGINE}" in
        chromium)
            return 0
            ;;
        firefox|webkit)
            fail "Unsupported engine '${ENGINE}'. Chromium only in this phase."
            ;;
        *)
            fail "Unsupported engine '${ENGINE}'. Chromium only in this phase."
            ;;
    esac
}

check_runtime_libraries() {
    local runtime_binary="${1:-}" ldd_output missing_lines
    [[ -n "${runtime_binary}" ]] || fail "internal error: missing runtime binary"

    if [[ -n "${BROWSER_PREFLIGHT_LDD_OUTPUT:-}" ]]; then
        ldd_output="${BROWSER_PREFLIGHT_LDD_OUTPUT}"
    else
        if ! ldd_output="$(ldd "${runtime_binary}" 2>&1)"; then
            fail "required runtime libraries are missing for ${runtime_binary}: unable to inspect dependencies"
        fi
    fi

    if grep -q 'not found' <<<"${ldd_output}"; then
        missing_lines="$(grep 'not found' <<<"${ldd_output}")"
        fail "required runtime libraries are missing for ${runtime_binary}: ${missing_lines}"
    fi

    if grep -Eq 'not a dynamic executable|statically linked' <<<"${ldd_output}"; then
        fail "required runtime libraries are missing for ${runtime_binary}: ${ldd_output}"
    fi
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --engine)
            [[ $# -ge 2 ]] || fail "--engine requires a value"
            ENGINE="$2"
            shift 2
            ;;
        --engine=*)
            ENGINE="${1#--engine=}"
            shift
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            fail "unknown argument: $1"
            ;;
    esac
done

check_supported_engine

if ! CHROME_CANDIDATE="$(resolve_chromium_candidate)"; then
    fail "Chromium browser binary is missing: could not resolve a Chromium-family browser"
fi

CHROMIUM_RUNTIME=""
if ! CHROMIUM_RUNTIME="$(resolve_chromium_runtime "${CHROME_CANDIDATE}")"; then
    fail "Chromium browser binary is missing: ${CHROME_CANDIDATE}"
fi

check_runtime_libraries "${CHROMIUM_RUNTIME}"

printf 'READY: Chromium browser preflight passed (wrapper=%s runtime=%s libs=ok)\n' "${CHROME_CANDIDATE}" "${CHROMIUM_RUNTIME}"
