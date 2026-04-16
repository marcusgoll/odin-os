#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ODIN_DIR="${ODIN_DIR:-${REPO_ROOT}/.odin-browser}"
BROWSER_STATE_DIR="${ODIN_DIR}/browser-state"
BROWSER_SERVER_PORT="${ODIN_BROWSER_PORT:-19227}"
BROWSER_SERVER_URL="http://127.0.0.1:${BROWSER_SERVER_PORT}"
BROWSER_SERVER_SCRIPT="${SCRIPT_DIR}/odin-huginn-server.js"
BROWSER_SERVER_PID_FILE="${BROWSER_STATE_DIR}/browser.pid"
BROWSER_SERVER_LOG="${ODIN_DIR}/logs/$(date +%Y-%m-%d)/browser-runtime.log"

_ba_json() {
    jq -n "$@"
}

browser_request_domain_access() {
    return 0
}

_bc_curl() {
    curl -sf --max-time 30 "$@"
}

browser_server_start() {
    local url="" headless="true"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --url) url="$2"; shift 2 ;;
            --headless) headless="true"; shift ;;
            --headed) headless="false"; shift ;;
            *) url="$1"; shift ;;
        esac
    done

    mkdir -p "${BROWSER_STATE_DIR}" "$(dirname "${BROWSER_SERVER_LOG}")"

    if [[ -f "${BROWSER_SERVER_PID_FILE}" ]]; then
        local old_pid
        old_pid="$(cat "${BROWSER_SERVER_PID_FILE}" 2>/dev/null || true)"
        if [[ -n "${old_pid}" ]] && kill -0 "${old_pid}" 2>/dev/null; then
            kill "${old_pid}" 2>/dev/null || true
            sleep 1
        fi
        rm -f "${BROWSER_SERVER_PID_FILE}"
    fi

    ODIN_DIR="${ODIN_DIR}" ODIN_BROWSER_PORT="${BROWSER_SERVER_PORT}"         node "${BROWSER_SERVER_SCRIPT}" >> "${BROWSER_SERVER_LOG}" 2>&1 &
    local server_pid=$!
    printf '%s
' "${server_pid}" > "${BROWSER_SERVER_PID_FILE}"

    local attempt=0
    until _bc_curl "${BROWSER_SERVER_URL}/health" >/dev/null 2>&1; do
        if ! kill -0 "${server_pid}" 2>/dev/null; then
            rm -f "${BROWSER_SERVER_PID_FILE}"
            return 1
        fi
        attempt=$((attempt + 1))
        if [[ "${attempt}" -gt 30 ]]; then
            kill "${server_pid}" 2>/dev/null || true
            wait "${server_pid}" 2>/dev/null || true
            rm -f "${BROWSER_SERVER_PID_FILE}"
            return 1
        fi
        sleep 1
    done

    local launch_body
    launch_body="$(jq -n --arg url "${url}" --arg headless "${headless}" '{browser:"chromium", headless: ($headless == "true") } | if $url != "" then . + {url: $url} else . end')"
    if ! _bc_curl -X POST "${BROWSER_SERVER_URL}/launch" -H 'Content-Type: application/json' -d "${launch_body}" >/dev/null; then
        browser_server_stop >/dev/null 2>&1 || true
        return 1
    fi

    local health
    health="$(_bc_curl "${BROWSER_SERVER_URL}/health")"
    [[ "$(echo "${health}" | jq -r '.engine // empty')" == "chromium" ]]
}

browser_server_stop() {
    _bc_curl -X POST "${BROWSER_SERVER_URL}/stop" >/dev/null 2>&1 || true
    if [[ -f "${BROWSER_SERVER_PID_FILE}" ]]; then
        local pid
        pid="$(cat "${BROWSER_SERVER_PID_FILE}" 2>/dev/null || true)"
        if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
            kill "${pid}" 2>/dev/null || true
            sleep 1
            kill -9 "${pid}" 2>/dev/null || true
        fi
        rm -f "${BROWSER_SERVER_PID_FILE}"
    fi
}

browser_navigate() {
    local target="${1:-}"
    [[ -n "${target}" ]] || return 1
    local body
    body="$(jq -n --arg url "${target}" '{url: $url}')"
    _bc_curl -X POST "${BROWSER_SERVER_URL}/navigate" -H 'Content-Type: application/json' -d "${body}" >/dev/null
}

browser_snapshot() {
    local query=""
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --interactive|--compact) shift ;;
            *) shift ;;
        esac
    done
    _bc_curl "${BROWSER_SERVER_URL}/snapshot" | jq -r '.snapshot // empty'
}
