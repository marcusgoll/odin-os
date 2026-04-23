#!/usr/bin/env bash
# Minimal repo-local browser helpers for Odin live drivers.

[[ "${BASH_SOURCE[0]}" == "${0}" ]] && set -euo pipefail

BROWSER_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ODIN_DIR="${ODIN_DIR:-${ODIN_ROOT:-${HOME}/.odin}}"
BROWSER_STATE_DIR="${ODIN_DIR}/browser-state"
BROWSER_SERVER_PORT="${ODIN_BROWSER_PORT:-19227}"
BROWSER_SERVER_URL="${ODIN_BROWSER_SERVER_URL:-http://127.0.0.1:${BROWSER_SERVER_PORT}}"
BROWSER_SERVER_SCRIPT="${ODIN_BROWSER_SERVER_SCRIPT:-${BROWSER_LIB_DIR}/odin-huginn-server.js}"
BROWSER_CHROME_CDP_LIB="${ODIN_CHROME_CDP_LIB_PATH:-${BROWSER_LIB_DIR}/chrome-cdp-start.sh}"
BROWSER_SERVER_PID_FILE="${BROWSER_STATE_DIR}/browser.pid"
BROWSER_SERVER_PORT_FILE="${BROWSER_STATE_DIR}/browser.port"
BROWSER_SERVER_LOG="${ODIN_DIR}/logs/$(date +%Y-%m-%d)/browser-runtime.log"
BROWSER_CURL_MAX_TIME="${ODIN_BROWSER_CURL_MAX_TIME:-30}"
BROWSER_PROBE_CURL_MAX_TIME="${ODIN_BROWSER_PROBE_CURL_MAX_TIME:-5}"
BROWSER_START_TIMEOUT="${ODIN_BROWSER_START_TIMEOUT:-20}"
_BA_SERVER_OWNED="${_BA_SERVER_OWNED:-0}"
_BA_CDP_OWNED="${_BA_CDP_OWNED:-0}"
_BA_PRESERVE_TRUSTED_SESSION="${_BA_PRESERVE_TRUSTED_SESSION:-0}"
_BA_BROWSER_PORT_EXPLICIT="${_BA_BROWSER_PORT_EXPLICIT:-0}"

if [[ -n "${ODIN_BROWSER_PORT:-}" ]]; then
    _BA_BROWSER_PORT_EXPLICIT=1
fi

_bc_curl_with_max_time() {
    local max_time="${1:-${BROWSER_CURL_MAX_TIME}}"
    shift || true
    curl -sf --max-time "${max_time}" "$@"
}

_bc_curl() {
    _bc_curl_with_max_time "${BROWSER_CURL_MAX_TIME}" "$@"
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

_ba_domain_allowed() {
    python3 - "$1" "${ODIN_BROWSER_DOMAIN_DENYLIST:-localhost,127.0.0.1,::1,*.local}" <<'PY'
from urllib.parse import urlparse
import ipaddress
import sys

target = sys.argv[1].strip()
denylist = [entry.strip().lower() for entry in sys.argv[2].split(",") if entry.strip()]
if not target:
    raise SystemExit(1)

parsed = urlparse(target)
scheme = (parsed.scheme or "").lower()
if scheme in {"data", "about"}:
    raise SystemExit(0)
if scheme not in {"http", "https"}:
    raise SystemExit(1)

host = (parsed.hostname or "").strip().lower().rstrip(".")
if not host:
    raise SystemExit(1)
if host == "localhost" or host.endswith(".localhost"):
    raise SystemExit(1)

try:
    address = ipaddress.ip_address(host)
except ValueError:
    address = None

if address is not None and (address.is_loopback or address.is_private or address.is_link_local):
    raise SystemExit(1)

for entry in denylist:
    if entry.startswith("*."):
        suffix = entry[2:]
        if host == suffix or host.endswith("." + suffix):
            raise SystemExit(1)
    elif host == entry:
        raise SystemExit(1)
PY
}

browser_request_domain_access() {
    local target="${1:-}"
    [[ -n "${target}" ]] || return 1
    _ba_domain_allowed "${target}"
}

_ba_source_chrome_cdp_lib() {
    [[ -f "${BROWSER_CHROME_CDP_LIB}" ]] || return 1
    # shellcheck source=/dev/null
    source "${BROWSER_CHROME_CDP_LIB}"
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
    [[ -n "${port}" ]] || return 1
    printf 'http://127.0.0.1:%s' "${port}"
}

_ba_external_browser_server_url() {
    printf '%s' "${ODIN_BROWSER_SERVER_URL:-}"
}

_ba_using_external_browser_server() {
    [[ -n "$(_ba_external_browser_server_url)" ]]
}

if _ba_using_external_browser_server; then
    BROWSER_SERVER_PORT=""
    BROWSER_SERVER_URL="$(_ba_external_browser_server_url)"
elif [[ "${_BA_BROWSER_PORT_EXPLICIT}" == "0" ]]; then
    ODIN_BROWSER_PORT="$(_ba_resolve_free_port)"
    BROWSER_SERVER_PORT="${ODIN_BROWSER_PORT}"
    BROWSER_SERVER_URL="http://127.0.0.1:${BROWSER_SERVER_PORT}"
fi

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
    [[ -n "${port}" ]] || return 1

    proc_root="$(_ba_proc_root)"
    _ba_proc_has_exact_entry "${proc_root}/${pid}/cmdline" "${BROWSER_SERVER_SCRIPT}" || return 1
    _ba_proc_has_exact_entry "${proc_root}/${pid}/environ" "ODIN_DIR=${ODIN_DIR}" || return 1
    _ba_proc_has_exact_entry "${proc_root}/${pid}/environ" "ODIN_BROWSER_PORT=${port}" || return 1
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

_ba_browser_launch() {
    local url="${1:-}" headless="${2:-true}" body

    body="$(jq -nc --arg browser "chromium" --arg url "${url}" --arg headless "${headless}" '
        {
            browser: $browser,
            headless: ($headless == "true")
        }
        | if $url != "" then . + {url: $url} else . end
    ')"

    _bc_curl \
        -X POST \
        -H 'Content-Type: application/json' \
        -d "${body}" \
        "${BROWSER_SERVER_URL}/launch" >/dev/null
}

browser_server_health() {
    _bc_curl "${BROWSER_SERVER_URL}/health"
}

_ba_server_page_is_responsive() {
    local body response

    body="$(jq -nc --arg fn '({ url: location.href, title: document.title })' '{fn: $fn}')"
    response="$(_bc_curl_with_max_time \
        "${BROWSER_PROBE_CURL_MAX_TIME}" \
        -X POST \
        -H 'Content-Type: application/json' \
        -d "${body}" \
        "${BROWSER_SERVER_URL}/evaluate")" || return 1
    jq -e '.ok == true' <<<"${response}" >/dev/null 2>&1
}

_ba_server_has_attached_external_page() {
    local health_json=""

    health_json="$(browser_server_health 2>/dev/null)" || return 1
    jq -e '.ok == true and .page == true and .browser == false' <<<"${health_json}" >/dev/null 2>&1 || return 1
    _ba_server_page_is_responsive >/dev/null 2>&1
}

browser_server_connect_cdp() {
    local cdp_url="${1:-}" target_url="${2:-}" body

    [[ -n "${cdp_url}" ]] || return 1
    body="$(jq -nc --arg cdp_url "${cdp_url}" --arg url "${target_url}" '
        {cdpUrl: $cdp_url}
        | if $url != "" then . + {url: $url} else . end
    ')"

    _bc_curl \
        -X POST \
        -H 'Content-Type: application/json' \
        -d "${body}" \
        "${BROWSER_SERVER_URL}/connect" >/dev/null
}

_ba_wait_for_server() {
    local attempt=0

    while (( attempt < BROWSER_START_TIMEOUT )); do
        if browser_server_health >/dev/null 2>&1; then
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done

    return 1
}

_bc_browser_server_start_attempt() {
    local url="${1:-}" headless="${2:-true}" server_pid="" attempt=0 launch_body="" health=""

    ODIN_DIR="${ODIN_DIR}" ODIN_BROWSER_PORT="${BROWSER_SERVER_PORT}" node "${BROWSER_SERVER_SCRIPT}" >> "${BROWSER_SERVER_LOG}" 2>&1 &
    server_pid=$!
    printf '%s %s\n' "${server_pid}" "${BROWSER_SERVER_PORT}" > "${BROWSER_SERVER_PID_FILE}"
    printf '%s\n' "${BROWSER_SERVER_PORT}" > "${BROWSER_SERVER_PORT_FILE}"

    until _bc_curl "${BROWSER_SERVER_URL}/health" >/dev/null 2>&1; do
        if ! kill -0 "${server_pid}" 2>/dev/null; then
            return 1
        fi
        attempt=$((attempt + 1))
        if [[ "${attempt}" -gt 30 ]]; then
            return 1
        fi
        sleep 1
    done

    launch_body="$(jq -nc \
        --arg url "${url}" \
        --arg headless "${headless}" '
        {
            browser: "chromium",
            headless: ($headless == "true")
        }
        | if $url != "" then . + {url: $url} else . end
    ')"
    if ! _bc_curl -X POST "${BROWSER_SERVER_URL}/launch" -H 'Content-Type: application/json' -d "${launch_body}" >/dev/null; then
        return 1
    fi

    health="$(_bc_curl "${BROWSER_SERVER_URL}/health")" || return 1
    [[ "$(jq -r '.engine // empty' <<<"${health}")" == "chromium" ]]
}

browser_server_start() {
    local target_url="" headless="true"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --url)
                target_url="${2:-}"
                shift 2
                ;;
            --headless)
                headless="true"
                shift
                ;;
            --headed)
                headless="false"
                shift
                ;;
            *)
                target_url="$1"
                shift
                ;;
        esac
    done

    if [[ -n "${target_url}" ]]; then
        browser_request_domain_access "${target_url}" || return 1
    fi

    if _ba_using_external_browser_server; then
        browser_server_health >/dev/null 2>&1 || return 1
        _BA_SERVER_OWNED=0
        return 0
    fi

    mkdir -p "${BROWSER_STATE_DIR}" "$(dirname "${BROWSER_SERVER_LOG}")"

    if browser_server_health >/dev/null 2>&1; then
        _BA_SERVER_OWNED=0
        _ba_browser_launch "${target_url}" "${headless}"
        return 0
    fi

    if [[ -f "${BROWSER_SERVER_PID_FILE}" || -f "${BROWSER_SERVER_PORT_FILE}" ]]; then
        browser_server_stop >/dev/null 2>&1 || true
    fi

    if ! command -v node >/dev/null 2>&1; then
        return 1
    fi
    if [[ ! -f "${BROWSER_SERVER_SCRIPT}" ]]; then
        return 1
    fi

    local attempt=1 max_attempts=3
    while true; do
        if _bc_browser_server_start_attempt "${target_url}" "${headless}"; then
            _BA_SERVER_OWNED=1
            return 0
        fi
        browser_server_stop >/dev/null 2>&1 || true
        if [[ "${attempt}" -ge "${max_attempts}" ]]; then
            return 1
        fi
        attempt=$((attempt + 1))
        if [[ "${_BA_BROWSER_PORT_EXPLICIT}" == "0" ]]; then
            ODIN_BROWSER_PORT="$(_ba_resolve_free_port)"
            BROWSER_SERVER_PORT="${ODIN_BROWSER_PORT}"
            BROWSER_SERVER_URL="http://127.0.0.1:${BROWSER_SERVER_PORT}"
        fi
    done
}

browser_trusted_session_start() {
    local target_url="" cdp_url="${ODIN_CHROME_CDP_URL:-}" server_pid=""

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --url)
                target_url="${2:-}"
                shift 2
                ;;
            --cdp-url)
                cdp_url="${2:-}"
                shift 2
                ;;
            *)
                target_url="$1"
                shift
                ;;
        esac
    done

    if [[ -n "${target_url}" ]]; then
        browser_request_domain_access "${target_url}" || return 1
    fi

    _BA_PRESERVE_TRUSTED_SESSION=0

    if ! browser_server_health >/dev/null 2>&1; then
        if _ba_using_external_browser_server; then
            return 1
        fi
        BROWSER_SERVER_URL="http://127.0.0.1:${BROWSER_SERVER_PORT}"
    fi

    if _ba_server_has_attached_external_page; then
        _BA_PRESERVE_TRUSTED_SESSION=1
        if [[ -n "${target_url}" ]]; then
            browser_navigate "${target_url}" >/dev/null 2>&1 || return 1
        fi
        return 0
    fi

    if [[ -z "${cdp_url}" ]]; then
        _ba_source_chrome_cdp_lib || return 1
        cdp_start || return 1
        cdp_url="http://127.0.0.1:${CHROME_CDP_PORT:-${ODIN_CHROME_CDP_PORT:-9222}}"
        _BA_CDP_OWNED="${ODIN_CHROME_CDP_OWNED:-1}"
    fi

    mkdir -p "${BROWSER_STATE_DIR}" "$(dirname "${BROWSER_SERVER_LOG}")"

    if ! browser_server_health >/dev/null 2>&1; then
        if ! command -v node >/dev/null 2>&1; then
            return 1
        fi
        if [[ ! -f "${BROWSER_SERVER_SCRIPT}" ]]; then
            return 1
        fi

        if [[ -f "${BROWSER_SERVER_PID_FILE}" || -f "${BROWSER_SERVER_PORT_FILE}" ]]; then
            browser_server_stop >/dev/null 2>&1 || true
        fi

        ODIN_DIR="${ODIN_DIR}" ODIN_BROWSER_PORT="${BROWSER_SERVER_PORT}" node "${BROWSER_SERVER_SCRIPT}" >> "${BROWSER_SERVER_LOG}" 2>&1 &
        server_pid=$!
        printf '%s %s\n' "${server_pid}" "${BROWSER_SERVER_PORT}" >"${BROWSER_SERVER_PID_FILE}"
        printf '%s\n' "${BROWSER_SERVER_PORT}" >"${BROWSER_SERVER_PORT_FILE}"
        _BA_SERVER_OWNED=1

        if ! _ba_wait_for_server; then
            browser_server_stop >/dev/null 2>&1 || true
            return 1
        fi
    fi

    if ! browser_server_connect_cdp "${cdp_url}" "${target_url}"; then
        browser_server_stop >/dev/null 2>&1 || true
        return 1
    fi

    _BA_PRESERVE_TRUSTED_SESSION=1

    return 0
}

browser_server_stop() {
    local pid="" port="" stop_url=""

    if [[ "${_BA_PRESERVE_TRUSTED_SESSION}" == "1" ]]; then
        _BA_SERVER_OWNED=0
        _BA_CDP_OWNED=0
        return 0
    fi

    if ! _ba_using_external_browser_server; then
        if [[ -f "${BROWSER_SERVER_PID_FILE}" || -f "${BROWSER_SERVER_PORT_FILE}" ]]; then
            local runtime_state=()
            mapfile -t runtime_state < <(_ba_browser_runtime_state)
            pid="${runtime_state[0]:-}"
            port="${runtime_state[1]:-}"
        fi

        if [[ -z "${port}" && -n "${BROWSER_SERVER_PORT}" ]]; then
            port="${BROWSER_SERVER_PORT}"
        fi

        if [[ -n "${pid}" && -n "${port}" ]] && _ba_pid_is_browser_runtime "${pid}" "${port}"; then
            stop_url="$(_ba_browser_runtime_url "${port}")"
            _bc_curl -X POST "${stop_url}/stop" >/dev/null 2>&1 || true
            _ba_stop_pid_if_runtime "${pid}" "${port}"
        elif [[ "${_BA_SERVER_OWNED}" == "1" ]]; then
            _bc_curl -X POST "${BROWSER_SERVER_URL}/stop" >/dev/null 2>&1 || true
        fi

        rm -f "${BROWSER_SERVER_PID_FILE}" "${BROWSER_SERVER_PORT_FILE}"
        _BA_SERVER_OWNED=0
    fi

    if [[ "${_BA_CDP_OWNED}" == "1" ]] && declare -F cdp_stop >/dev/null 2>&1; then
        cdp_stop >/dev/null 2>&1 || true
        _BA_CDP_OWNED=0
    fi
}

browser_navigate() {
    local target_url="" action="" body response

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --reload)
                action="reload"
                shift
                ;;
            *)
                target_url="$1"
                shift
                ;;
        esac
    done

    if [[ -n "${target_url}" ]]; then
        browser_request_domain_access "${target_url}" || return 1
    fi
    browser_server_health >/dev/null

    body="$(jq -nc \
        --arg url "${target_url}" \
        --arg action "${action}" \
        'if $action != "" then {action: $action} else {url: $url} end')"
    response="$(_bc_curl \
        -X POST \
        -H 'Content-Type: application/json' \
        -d "${body}" \
        "${BROWSER_SERVER_URL}/navigate")" || return 1
    jq -e '.ok == true' <<<"${response}" >/dev/null
}

browser_current_url() {
    _bc_curl "${BROWSER_SERVER_URL}/health" | jq -r '.url // empty'
}

browser_snapshot() {
    local response=""

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --interactive|--compact)
                shift
                ;;
            *)
                shift
                ;;
        esac
    done

    response="$(_bc_curl "${BROWSER_SERVER_URL}/snapshot")" || return 1
    jq -r '.snapshot // empty' <<<"${response}"
}

browser_evaluate() {
    local fn="${1:-}" body response

    [[ -n "${fn}" ]] || return 1
    body="$(jq -nc --arg fn "${fn}" '{fn: $fn}')"
    response="$(_bc_curl \
        -X POST \
        -H 'Content-Type: application/json' \
        -d "${body}" \
        "${BROWSER_SERVER_URL}/evaluate")" || return 1
    jq -c '.result' <<<"${response}"
}

browser_type_selector() {
    local selector="${1:-}" text="${2:-}"
    shift 2 || true
    local submit="false" body response

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --submit)
                submit="true"
                shift
                ;;
            *)
                shift
                ;;
        esac
    done

    [[ -n "${selector}" ]] || return 1

    body="$(jq -nc \
        --arg selector "${selector}" \
        --arg text "${text}" \
        --arg submit "${submit}" \
        '{
            kind: "type",
            selector: $selector,
            text: $text,
            submit: ($submit == "true")
        }')"
    response="$(_bc_curl \
        -X POST \
        -H 'Content-Type: application/json' \
        -d "${body}" \
        "${BROWSER_SERVER_URL}/act")" || return 1
    jq -e '.ok == true' <<<"${response}" >/dev/null
}

browser_click_selector() {
    local selector="${1:-}" body response

    [[ -n "${selector}" ]] || return 1
    browser_server_health >/dev/null

    body="$(jq -nc \
        --arg selector "${selector}" \
        '{
            kind: "click",
            selector: $selector
        }')"
    response="$(_bc_curl \
        -X POST \
        -H 'Content-Type: application/json' \
        -d "${body}" \
        "${BROWSER_SERVER_URL}/act")" || return 1
    jq -e '.ok == true' <<<"${response}" >/dev/null
}

browser_bc_screenshot() {
    local output_path="" body response screenshot_path

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --output)
                output_path="${2:-}"
                shift 2
                ;;
            *)
                shift
                ;;
        esac
    done

    if [[ -z "${output_path}" ]]; then
        output_path="${BROWSER_STATE_DIR}/browser.png"
    fi
    mkdir -p "$(dirname "${output_path}")"

    body="$(jq -nc --arg path "${output_path}" '{path: $path}')"
    response="$(_bc_curl \
        -X POST \
        -H 'Content-Type: application/json' \
        -d "${body}" \
        "${BROWSER_SERVER_URL}/screenshot")" || return 1
    screenshot_path="$(jq -r '.screenshot_path // empty' <<<"${response}")"
    [[ -n "${screenshot_path}" ]] || return 1
    printf '%s' "${screenshot_path}"
}
