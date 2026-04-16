#!/usr/bin/env bash
# Minimal browser access helpers for odin-os live drivers.

[[ "${BASH_SOURCE[0]}" == "${0}" ]] && set -euo pipefail

ODIN_DIR="${ODIN_DIR:-${HOME}/.odin}"
BROWSER_STATE_DIR="${ODIN_DIR}/browser-state"
BROWSER_SERVER_PORT="${ODIN_BROWSER_PORT:-9227}"
BROWSER_SERVER_URL="${ODIN_BROWSER_SERVER_URL:-http://127.0.0.1:${BROWSER_SERVER_PORT}}"
BROWSER_SERVER_PID_FILE="${BROWSER_STATE_DIR}/odin-browser-server.pid"
BROWSER_SERVER_SCRIPT="${ODIN_BROWSER_SERVER_SCRIPT:-}"
BROWSER_SESSION_DIR="${ODIN_BROWSER_SESSION_DIR:-${BROWSER_STATE_DIR}/sessions}"
BROWSER_CURL_MAX_TIME="${ODIN_BROWSER_CURL_MAX_TIME:-30}"
BROWSER_START_TIMEOUT="${ODIN_BROWSER_START_TIMEOUT:-15}"

_ba_trace() {
    if [[ -n "${ODIN_TEST_HUGINN_TRACE:-}" ]]; then
        printf '%s\n' "$1" >>"${ODIN_TEST_HUGINN_TRACE}"
    fi
}

_ba_current_project() {
    printf '%s\n' "${ODIN_PROJECT:-${ODIN_PROJECT_ID:-default}}"
}

_ba_current_profile() {
    printf '%s\n' "${ODIN_BROWSER_PROFILE:-default}"
}

_ba_agent_name() {
    printf '%s\n' "${ODIN_AGENT_NAME:-default}"
}

_ba_agent_dir() {
    printf '%s\n' "${ODIN_DIR}/agents/$(_ba_agent_name)"
}

_ba_session_handle() {
    local service="$1"
    printf 'session://%s/%s/%s\n' "$(_ba_current_project)" "${service}" "$(_ba_current_profile)"
}

_ba_auth_token_file() {
    printf '%s\n' "$(_ba_agent_dir)/browser-auth-token"
}

_ba_read_auth_token() {
    if [[ -n "${HUGINN_AUTH_TOKEN:-}" ]]; then
        printf '%s' "${HUGINN_AUTH_TOKEN}"
        return 0
    fi
    if [[ -f "$(_ba_auth_token_file)" ]]; then
        tr -d '\n' <"$(_ba_auth_token_file)"
        return 0
    fi
    if [[ -f "${BROWSER_STATE_DIR}/auth-token" ]]; then
        tr -d '\n' <"${BROWSER_STATE_DIR}/auth-token"
    fi
}

_bc_curl() {
    if [[ "$*" == *"/health"* && -n "${ODIN_TEST_HUGINN_HEALTH:-}" ]]; then
        printf '%s' "${ODIN_TEST_HUGINN_HEALTH}"
        return 0
    fi

    local token args
    token="$(_ba_read_auth_token)"
    args=(-sf --max-time "${BROWSER_CURL_MAX_TIME}")
    if [[ -n "${token}" ]]; then
        args+=(-H "Authorization: Bearer ${token}")
    fi
    curl "${args[@]}" "$@"
}

browser_server_start() {
    local headless="true"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --headless) headless="true"; shift ;;
            --headed) headless="false"; shift ;;
            --url) shift 2 ;;
            *) shift ;;
        esac
    done

    if [[ -n "${ODIN_TEST_HUGINN_HEALTH:-}" ]]; then
        _ba_trace "start"
        return 0
    fi

    if _bc_curl "${BROWSER_SERVER_URL}/health" >/dev/null 2>&1; then
        return 0
    fi
    if ! command -v node >/dev/null 2>&1; then
        return 1
    fi
    if [[ -z "${BROWSER_SERVER_SCRIPT}" || ! -f "${BROWSER_SERVER_SCRIPT}" ]]; then
        return 1
    fi

    mkdir -p "${BROWSER_STATE_DIR}" "$(_ba_agent_dir)" >/dev/null 2>&1 || true
    ODIN_BROWSER_PORT="${BROWSER_SERVER_PORT}" \
    ODIN_DIR="${ODIN_DIR}" \
    ODIN_AGENT_NAME="$(_ba_agent_name)" \
        node "${BROWSER_SERVER_SCRIPT}" >/dev/null 2>&1 &
    local server_pid=$!
    printf '%s\n' "${server_pid}" >"${BROWSER_SERVER_PID_FILE}"

    local elapsed=0
    while (( elapsed < BROWSER_START_TIMEOUT )); do
        if _bc_curl "${BROWSER_SERVER_URL}/health" >/dev/null 2>&1; then
            return 0
        fi
        if ! kill -0 "${server_pid}" >/dev/null 2>&1; then
            return 1
        fi
        sleep 1
        elapsed=$(( elapsed + 1 ))
    done
    return 1
}

browser_server_stop() {
    if [[ -n "${ODIN_TEST_HUGINN_HEALTH:-}" ]]; then
        _ba_trace "stop"
        return 0
    fi

    _bc_curl -X POST "${BROWSER_SERVER_URL}/stop" >/dev/null 2>&1 || true
    if [[ -f "${BROWSER_SERVER_PID_FILE}" ]]; then
        local pid
        pid="$(cat "${BROWSER_SERVER_PID_FILE}" 2>/dev/null || true)"
        if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
            kill "${pid}" >/dev/null 2>&1 || true
            sleep 1
            kill -9 "${pid}" >/dev/null 2>&1 || true
        fi
        rm -f "${BROWSER_SERVER_PID_FILE}"
    fi
}

browser_load_session() {
    local service="${1:-}"
    [[ -n "${service}" ]] || return 1

    if [[ -n "${ODIN_TEST_HUGINN_HEALTH:-}" ]]; then
        _ba_trace "load:${service}"
        return 0
    fi

    local cookie_file="${BROWSER_SESSION_DIR}/${service}.json"
    [[ -f "${cookie_file}" ]] || return 1

    local payload
    payload="$(python3 - "${cookie_file}" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as handle:
    payload = json.load(handle)
if isinstance(payload, list):
    payload = {"cookies": payload}
print(json.dumps(payload))
PY
)" || return 1

    _bc_curl -X POST \
        -H "Content-Type: application/json" \
        -d "${payload}" \
        "${BROWSER_SERVER_URL}/cookies/set" >/dev/null
}

browser_navigate() {
    local url="$1"
    [[ -n "${url}" ]] || return 1

    if [[ -n "${ODIN_TEST_HUGINN_HEALTH:-}" ]]; then
        _ba_trace "navigate:${url}"
        return 0
    fi

    local payload
    payload="$(python3 - "${url}" <<'PY'
import json
import sys
print(json.dumps({"url": sys.argv[1]}))
PY
)"
    _bc_curl -X POST \
        -H "Content-Type: application/json" \
        -d "${payload}" \
        "${BROWSER_SERVER_URL}/navigate" >/dev/null
}

browser_snapshot() {
    if [[ -n "${ODIN_TEST_HUGINN_SNAPSHOT:-}" ]]; then
        printf '%s' "${ODIN_TEST_HUGINN_SNAPSHOT}"
        return 0
    fi

    local response
    response="$(_bc_curl "${BROWSER_SERVER_URL}/snapshot?compact=true")" || return 1
    RESPONSE_BODY="${response}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["RESPONSE_BODY"])
print(payload.get("snapshot") or "", end="")
PY
}
