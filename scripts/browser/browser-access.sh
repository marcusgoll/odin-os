#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ODIN_DIR="${ODIN_DIR:-${REPO_ROOT}/.odin-browser}"
BROWSER_STATE_DIR="${ODIN_DIR}/browser-state"
BROWSER_SERVER_SCRIPT="${SCRIPT_DIR}/odin-huginn-server.js"
BROWSER_SERVER_PID_FILE="${BROWSER_STATE_DIR}/browser.pid"
BROWSER_SERVER_PORT_FILE="${BROWSER_STATE_DIR}/browser.port"
BROWSER_SERVER_LOG="${ODIN_DIR}/logs/$(date +%Y-%m-%d)/browser-runtime.log"

# Minimal phase-36 shim for the repo-local Chromium runtime.
# This intentionally exposes only the browser cutover surface needed here.

_ba_json() {
    jq -n "$@"
}

_ba_resolve_free_port() {
    node <<'NODE'
const net = require('node:net');
const server = net.createServer();

server.on('error', () => process.exit(1));
server.listen(0, '127.0.0.1', () => {
  const address = server.address();
  const port = address && typeof address === 'object' ? address.port : null;
  server.close(() => {
    if (port) {
      console.log(port);
      return;
    }
    process.exit(1);
  });
});
NODE
}

_ba_browser_domain_denylist() {
    printf '%s' "${ODIN_BROWSER_DOMAIN_DENYLIST:-localhost,127.0.0.1,::1,*.local}"
}

_ba_domain_host() {
    local target="${1:-}" host
    [[ -n "${target}" ]] || return 1

    host="${target#*://}"
    if [[ "${host}" == "${target}" ]]; then
        host="${target}"
    fi
    host="${host%%[/?#]*}"
    host="${host##*@}"
    case "${host}" in
        \[*\]:*)
            host="${host#\[}"
            host="${host%%]*}"
            ;;
        \[*\])
            host="${host#\[}"
            host="${host%]}"
            ;;
        *:*)
            host="${host%%:*}"
            ;;
    esac

    host="${host,,}"
    [[ -n "${host}" ]] || return 1
    printf '%s' "${host}"
}

browser_request_domain_access() {
    local target="${1:-}" host denylist entry suffix
    [[ -n "${target}" ]] || return 1

    host="$(_ba_domain_host "${target}")" || return 1
    local IFS=','
    read -r -a denylist <<< "$(_ba_browser_domain_denylist)"
    for entry in "${denylist[@]}"; do
        entry="${entry//[[:space:]]/}"
        [[ -n "${entry}" ]] || continue
        entry="${entry,,}"
        case "${entry}" in
            \*.*)
                suffix="${entry#*.}"
                [[ "${host}" == "${suffix}" || "${host}" == *".${suffix}" ]] && return 1
                ;;
            *)
                [[ "${host}" == "${entry}" ]] && return 1
                ;;
        esac
    done

    return 0
}

_ba_browser_runtime_state() {
    local pid="" port=""

    if [[ -f "${BROWSER_SERVER_PID_FILE}" ]]; then
        read -r pid port < "${BROWSER_SERVER_PID_FILE}" || true
    fi
    if [[ -z "${port}" && -f "${BROWSER_SERVER_PORT_FILE}" ]]; then
        port="$(cat "${BROWSER_SERVER_PORT_FILE}" 2>/dev/null || true)"
    fi

    printf '%s\n%s\n' "${pid}" "${port}"
}

_ba_browser_runtime_url() {
    local port="${1:-}"
    if [[ -n "${port}" ]]; then
        printf 'http://127.0.0.1:%s' "${port}"
    else
        printf '%s' "${BROWSER_SERVER_URL}"
    fi
}

if [[ -z "${ODIN_BROWSER_PORT:-}" ]]; then
    ODIN_BROWSER_PORT="$(_ba_resolve_free_port)"
fi

BROWSER_SERVER_PORT="${ODIN_BROWSER_PORT}"
BROWSER_SERVER_URL="http://127.0.0.1:${BROWSER_SERVER_PORT}"

_bc_curl() {
    curl -sf --max-time 30 "$@"
}

_ba_proc_root() {
    printf '%s' "${BA_PROC_ROOT:-/proc}"
}

_ba_proc_has_exact_entry() {
    local path="${1:-}" expected="${2:-}" entry
    [[ -n "${path}" ]] || return 1
    [[ -n "${expected}" ]] || return 1
    [[ -r "${path}" ]] || return 1

    while IFS= read -r -d '' entry; do
        [[ "${entry}" == "${expected}" ]] && return 0
    done < "${path}"

    return 1
}

_ba_pid_is_browser_runtime() {
    local pid="${1:-}" port="${2:-}" proc_root
    [[ -n "${pid}" ]] || return 1
    [[ "${pid}" =~ ^[0-9]+$ ]] || return 1

    proc_root="$(_ba_proc_root)"
    _ba_proc_has_exact_entry "${proc_root}/${pid}/cmdline" "${BROWSER_SERVER_SCRIPT}" || return 1
    _ba_proc_has_exact_entry "${proc_root}/${pid}/environ" "ODIN_DIR=${ODIN_DIR}" || return 1
    _ba_proc_has_exact_entry "${proc_root}/${pid}/environ" "ODIN_BROWSER_PORT=${port:-${BROWSER_SERVER_PORT}}" || return 1
}

_ba_stop_pid_if_runtime() {
    local pid="${1:-}" port="${2:-}"
    if _ba_pid_is_browser_runtime "${pid}" "${port}"; then
        kill "${pid}" 2>/dev/null || true
        sleep 1
        if _ba_pid_is_browser_runtime "${pid}" "${port}"; then
            kill -9 "${pid}" 2>/dev/null || true
        fi
    fi
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

    if [[ -n "${url}" ]]; then
        browser_request_domain_access "${url}" || return 1
    fi

    mkdir -p "${BROWSER_STATE_DIR}" "$(dirname "${BROWSER_SERVER_LOG}")"

    if [[ -f "${BROWSER_SERVER_PID_FILE}" || -f "${BROWSER_SERVER_PORT_FILE}" ]]; then
        browser_server_stop >/dev/null 2>&1 || true
    fi

    ODIN_DIR="${ODIN_DIR}" ODIN_BROWSER_PORT="${BROWSER_SERVER_PORT}" node "${BROWSER_SERVER_SCRIPT}" >> "${BROWSER_SERVER_LOG}" 2>&1 &
    local server_pid=$!
    printf '%s\n' "${server_pid}" > "${BROWSER_SERVER_PID_FILE}"
    printf '%s\n' "${BROWSER_SERVER_PORT}" > "${BROWSER_SERVER_PORT_FILE}"

    local attempt=0
    until _bc_curl "${BROWSER_SERVER_URL}/health" >/dev/null 2>&1; do
        if ! kill -0 "${server_pid}" 2>/dev/null; then
            rm -f "${BROWSER_SERVER_PID_FILE}" "${BROWSER_SERVER_PORT_FILE}"
            return 1
        fi
        attempt=$((attempt + 1))
        if [[ "${attempt}" -gt 30 ]]; then
            kill "${server_pid}" 2>/dev/null || true
            wait "${server_pid}" 2>/dev/null || true
            rm -f "${BROWSER_SERVER_PID_FILE}" "${BROWSER_SERVER_PORT_FILE}"
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
    local pid="" port="" stop_url
    if [[ -f "${BROWSER_SERVER_PID_FILE}" || -f "${BROWSER_SERVER_PORT_FILE}" ]]; then
        local runtime_state
        mapfile -t runtime_state < <(_ba_browser_runtime_state)
        pid="${runtime_state[0]:-}"
        port="${runtime_state[1]:-}"
    fi

    stop_url="$(_ba_browser_runtime_url "${port}")"
    _bc_curl -X POST "${stop_url}/stop" >/dev/null 2>&1 || true
    if [[ -n "${pid}" ]]; then
        _ba_stop_pid_if_runtime "${pid}" "${port}"
    fi
    rm -f "${BROWSER_SERVER_PID_FILE}" "${BROWSER_SERVER_PORT_FILE}"
}

browser_navigate() {
    local target="${1:-}"
    [[ -n "${target}" ]] || return 1
    browser_request_domain_access "${target}" || return 1
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
