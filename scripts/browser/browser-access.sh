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

_ba_target_scheme() {
    local target="${1:-}"
    [[ -n "${target}" ]] || return 1

    if [[ "${target}" =~ ^([[:alpha:]][[:alnum:]+.-]*): ]]; then
        printf '%s' "${BASH_REMATCH[1],,}"
        return 0
    fi

    return 1
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

    host="$(_ba_normalize_host_for_matching "${host}")"
    [[ -n "${host}" ]] || return 1
    printf '%s' "${host}"
}

_ba_normalize_host_for_matching() {
    local host="${1:-}"
    [[ -n "${host}" ]] || return 1

    host="${host,,}"
    host="${host//%2e/.}"
    host="${host//%2E/.}"
    while [[ "${host}" == *. ]]; do
        host="${host%.}"
    done

    printf '%s' "${host}"
}

_ba_parse_numeric_host_part() {
    local value="${1:-}" parsed
    [[ -n "${value}" ]] || return 1

    parsed="$(printf '%d' "${value}" 2>/dev/null)" || return 1
    [[ "${parsed}" =~ ^[0-9]+$ ]] || return 1
    printf '%s' "${parsed}"
}

_ba_ipv4_is_loopback() {
    local host="${1:-}" a b c d
    [[ "${host}" =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]] || return 1

    a="${BASH_REMATCH[1]}"
    b="${BASH_REMATCH[2]}"
    c="${BASH_REMATCH[3]}"
    d="${BASH_REMATCH[4]}"
    [[ $((10#${a})) -le 255 && $((10#${b})) -le 255 && $((10#${c})) -le 255 && $((10#${d})) -le 255 ]] || return 1
    [[ $((10#${a})) -eq 127 ]]
}

_ba_ipv4_is_private_service() {
    local host="${1:-}" a b c d
    [[ "${host}" =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]] || return 1

    a="${BASH_REMATCH[1]}"
    b="${BASH_REMATCH[2]}"
    c="${BASH_REMATCH[3]}"
    d="${BASH_REMATCH[4]}"
    [[ $((10#${a})) -le 255 && $((10#${b})) -le 255 && $((10#${c})) -le 255 && $((10#${d})) -le 255 ]] || return 1
    case "${a}.${b}" in
        10.*|192.168)
            return 0
            ;;
        172.1[6-9]|172.2[0-9]|172.3[0-1])
            return 0
            ;;
    esac

    return 1
}

_ba_ipv4_value_is_local_service() {
    local value="${1:-}" a b
    [[ -n "${value}" ]] || return 1

    a=$(( (10#${value} >> 24) & 255 ))
    b=$(( (10#${value} >> 16) & 255 ))
    (( a == 127 || a == 10 || (a == 192 && b == 168) || (a == 172 && b >= 16 && b <= 31) ))
}

_ba_ipv4_alias_is_local_service() {
    local host="${1:-}" first second third fourth value
    local IFS='.'
    local -a parts=()

    [[ -n "${host}" ]] || return 1
    read -r -a parts <<< "${host}"
    case "${#parts[@]}" in
        1)
            value="$(_ba_parse_numeric_host_part "${parts[0]}")" || return 1
            (( value >= 0 && value <= 4294967295 )) || return 1
            _ba_ipv4_value_is_local_service "${value}"
            ;;
        2)
            first="$(_ba_parse_numeric_host_part "${parts[0]}")" || return 1
            second="$(_ba_parse_numeric_host_part "${parts[1]}")" || return 1
            [[ $((10#${first})) -le 255 && $((10#${second})) -le 16777215 ]] || return 1
            value=$(( (10#${first} << 24) | 10#${second} ))
            _ba_ipv4_value_is_local_service "${value}"
            ;;
        3)
            first="$(_ba_parse_numeric_host_part "${parts[0]}")" || return 1
            second="$(_ba_parse_numeric_host_part "${parts[1]}")" || return 1
            third="$(_ba_parse_numeric_host_part "${parts[2]}")" || return 1
            [[ $((10#${first})) -le 255 && $((10#${second})) -le 255 && $((10#${third})) -le 65535 ]] || return 1
            value=$(( (10#${first} << 24) | (10#${second} << 16) | 10#${third} ))
            _ba_ipv4_value_is_local_service "${value}"
            ;;
        4)
            first="$(_ba_parse_numeric_host_part "${parts[0]}")" || return 1
            second="$(_ba_parse_numeric_host_part "${parts[1]}")" || return 1
            third="$(_ba_parse_numeric_host_part "${parts[2]}")" || return 1
            fourth="$(_ba_parse_numeric_host_part "${parts[3]}")" || return 1
            [[ $((10#${first})) -le 255 && $((10#${second})) -le 255 && $((10#${third})) -le 255 && $((10#${fourth})) -le 255 ]] || return 1
            value=$(( (10#${first} << 24) | (10#${second} << 16) | (10#${third} << 8) | 10#${fourth} ))
            _ba_ipv4_value_is_local_service "${value}"
            ;;
        *)
            return 1
            ;;
    esac
}

_ba_ipv6_is_private_service() {
    local host="${1:-}" normalized
    [[ -n "${host}" ]] || return 1

    normalized="$(_ba_normalize_host_for_matching "${host}")"
    if [[ "${normalized}" == *:* ]]; then
        normalized="${normalized%%%*}"
    fi

    case "${normalized}" in
        ::1|fd*|fe8*|fe9*|fea*|feb*)
            return 0
            ;;
    esac

    return 1
}

_ba_ipv4_mapped_ipv6_is_local_service() {
    local host="${1:-}" tail hi lo v4
    [[ -n "${host}" ]] || return 1

    host="${host,,}"
    host="${host%.}"
    [[ "${host}" == ::ffff:* ]] || return 1

    tail="${host#::ffff:}"
    if _ba_ipv4_is_private_service "${tail}"; then
        return 0
    fi

    if _ba_ipv4_is_loopback "${tail}"; then
        return 0
    fi

    if [[ "${tail}" =~ ^([0-9a-f]{1,4}):([0-9a-f]{1,4})$ ]]; then
        hi=$((16#${BASH_REMATCH[1]}))
        lo=$((16#${BASH_REMATCH[2]}))
        v4=$(( (hi << 16) | lo ))
        _ba_ipv4_value_is_local_service "${v4}"
        return
    fi

    return 1
}

_ba_host_is_local_service() {
    local host="${1:-}" normalized
    [[ -n "${host}" ]] || return 1

    normalized="$(_ba_normalize_host_for_matching "${host}")"
    if [[ "${normalized}" == *:* ]]; then
        normalized="${normalized%%%*}"
    fi

    case "${normalized}" in
        localhost|*.localhost|::1)
            return 0
            ;;
    esac

    if _ba_ipv4_is_private_service "${normalized}"; then
        return 0
    fi

    if _ba_ipv4_alias_is_local_service "${normalized}"; then
        return 0
    fi

    if _ba_ipv6_is_private_service "${normalized}"; then
        return 0
    fi

    if _ba_ipv4_mapped_ipv6_is_local_service "${normalized}"; then
        return 0
    fi

    return 1
}

browser_request_domain_access() {
    local target="${1:-}" host scheme denylist entry suffix
    [[ -n "${target}" ]] || return 1

    scheme="$(_ba_target_scheme "${target}" || true)"
    case "${scheme}" in
        http|https|data|about)
            ;;
        *)
            return 1
            ;;
    esac

    case "${scheme}" in
        data|about)
            return 0
            ;;
    esac

    host="$(_ba_domain_host "${target}")" || return 1
    if _ba_host_is_local_service "${host}"; then
        return 1
    fi

    local IFS=","
    read -r -a denylist <<< "$(_ba_browser_domain_denylist)"
    for entry in "${denylist[@]}"; do
        entry="${entry//[[:space:]]/}"
        [[ -n "${entry}" ]] || continue
        entry="${entry,,}"
        case "${entry}" in
            \*.*)
                suffix="${entry#*.}"
                [[ "${host}" == "${suffix}" || "${host}" == *."${suffix}" ]] && return 1
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
    [[ -n "${port}" ]] || return 1
    printf 'http://127.0.0.1:%s' "${port}"
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

_bc_browser_server_start_attempt() {
    local url="${1:-}" headless="${2:-true}" server_pid attempt launch_body health

    ODIN_DIR="${ODIN_DIR}" ODIN_BROWSER_PORT="${BROWSER_SERVER_PORT}" node "${BROWSER_SERVER_SCRIPT}" >> "${BROWSER_SERVER_LOG}" 2>&1 &
    server_pid=$!
    printf '%s\n' "${server_pid}" > "${BROWSER_SERVER_PID_FILE}"
    printf '%s\n' "${BROWSER_SERVER_PORT}" > "${BROWSER_SERVER_PORT_FILE}"

    attempt=0
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

    launch_body="$(jq -n --arg url "${url}" --arg headless "${headless}" '{browser:"chromium", headless: ($headless == "true") } | if $url != "" then . + {url: $url} else . end')"
    if ! _bc_curl -X POST "${BROWSER_SERVER_URL}/launch" -H 'Content-Type: application/json' -d "${launch_body}" >/dev/null; then
        return 1
    fi

    health="$(_bc_curl "${BROWSER_SERVER_URL}/health")" || return 1
    [[ "$(echo "${health}" | jq -r '.engine // empty')" == "chromium" ]]
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

    local attempt=1 max_attempts=3
    while true; do
        if _bc_browser_server_start_attempt "${url}" "${headless}"; then
            return 0
        fi
        browser_server_stop >/dev/null 2>&1 || true
        if [[ "${attempt}" -ge "${max_attempts}" ]]; then
            return 1
        fi
        attempt=$((attempt + 1))
        ODIN_BROWSER_PORT="$(_ba_resolve_free_port)"
        BROWSER_SERVER_PORT="${ODIN_BROWSER_PORT}"
        BROWSER_SERVER_URL="http://127.0.0.1:${BROWSER_SERVER_PORT}"
    done
}

browser_server_stop() {
    local pid="" port="" stop_url
    if [[ -f "${BROWSER_SERVER_PID_FILE}" || -f "${BROWSER_SERVER_PORT_FILE}" ]]; then
        local runtime_state
        mapfile -t runtime_state < <(_ba_browser_runtime_state)
        pid="${runtime_state[0]:-}"
        port="${runtime_state[1]:-}"
    fi

    if [[ -n "${pid}" && -n "${port}" ]]; then
        if _ba_pid_is_browser_runtime "${pid}" "${port}"; then
            stop_url="$(_ba_browser_runtime_url "${port}")"
            _bc_curl -X POST "${stop_url}/stop" >/dev/null 2>&1 || true
            _ba_stop_pid_if_runtime "${pid}" "${port}"
        fi
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

browser_bc_screenshot() {
    local output_path="" body response screenshot_path
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --output)
                output_path="$2"
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
    response="$(_bc_curl -X POST "${BROWSER_SERVER_URL}/screenshot" -H 'Content-Type: application/json' -d "${body}")" || return 1
    screenshot_path="$(jq -r '.screenshot_path // empty' <<<"${response}")"
    [[ -n "${screenshot_path}" ]] || return 1
    printf '%s' "${screenshot_path}"
}

browser_server_health() {
    _bc_curl "${BROWSER_SERVER_URL}/health"
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
