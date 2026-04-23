#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_CDP_LIB="${SCRIPT_DIR}/../browser/chrome-cdp-start.sh"
DEFAULT_CLIENT_HTML="${SCRIPT_DIR}/trusted-browser-access/index.html"

ODIN_DIR="${ODIN_DIR:-${ODIN_ROOT:-/var/odin}}"
STATE_DIR="${ODIN_DIR}/browser-state/trusted-browser-access"
WEB_ROOT="${STATE_DIR}/web"
X11VNC_PID_FILE="${STATE_DIR}/x11vnc.pid"
WEBSOCKIFY_PID_FILE="${STATE_DIR}/websockify.pid"
X11VNC_LOG="${STATE_DIR}/x11vnc.log"
WEBSOCKIFY_LOG="${STATE_DIR}/websockify.log"
X11VNC_BIN="${ODIN_X11VNC_BIN:-x11vnc}"
WEBSOCKIFY_BIN="${ODIN_WEBSOCKIFY_BIN:-websockify}"
CDP_LIB_PATH="${ODIN_CHROME_CDP_LIB_PATH:-${DEFAULT_CDP_LIB}}"
CLIENT_HTML="${ODIN_TRUSTED_BROWSER_CLIENT_HTML:-${DEFAULT_CLIENT_HTML}}"
VNC_PORT="${ODIN_TRUSTED_BROWSER_VNC_PORT:-5901}"
NOVNC_PORT="${ODIN_TRUSTED_BROWSER_NOVNC_PORT:-6080}"
BIND_HOST="${ODIN_TRUSTED_BROWSER_BIND_HOST:-127.0.0.1}"
CHROME_DISPLAY="${ODIN_CHROME_DISPLAY:-:99}"
CHROME_PROFILE_DIR="${ODIN_DIR}/browser-state/chrome-profile"

die() {
    echo "$*" >&2
    exit 1
}

pid_running() {
    local pid_file="${1:-}" pid=""
    [[ -f "${pid_file}" ]] || return 1
    pid="$(cat "${pid_file}" 2>/dev/null || true)"
    [[ -n "${pid}" ]] || return 1
    kill -0 "${pid}" >/dev/null 2>&1
}

require_file() {
    local path="${1:-}" label="${2:-file}"
    [[ -f "${path}" ]] || die "missing ${label}: ${path}"
}

resolve_novnc_assets_dir() {
    local candidate=""

    for candidate in \
        "${ODIN_TRUSTED_BROWSER_NOVNC_ASSETS_DIR:-}" \
        "${HOME}/tmp/swa-vnc/noVNC-src"
    do
        [[ -n "${candidate}" ]] || continue
        if [[ -f "${candidate}/core/rfb.js" ]]; then
            printf '%s\n' "${candidate}"
            return 0
        fi
    done

    return 1
}

ensure_web_root() {
    local assets_dir="${1:-}"

    mkdir -p "${WEB_ROOT}"
    require_file "${CLIENT_HTML}" "client html"
    cp "${CLIENT_HTML}" "${WEB_ROOT}/index.html"

    if [[ -n "${assets_dir}" ]]; then
        ln -sfn "${assets_dir}" "${WEB_ROOT}/noVNC-src"
    else
        rm -f "${WEB_ROOT}/noVNC-src"
    fi
}

load_cdp_lib() {
    require_file "${CDP_LIB_PATH}" "chrome cdp library"
    # shellcheck source=/dev/null
    source "${CDP_LIB_PATH}"
}

start_trusted_browser() {
    mkdir -p "${ODIN_DIR}/logs/$(date +%Y-%m-%d)"
    load_cdp_lib
    cdp_start || die "unable to start trusted chrome cdp session"
}

start_x11vnc() {
    mkdir -p "${STATE_DIR}"
    if pid_running "${X11VNC_PID_FILE}"; then
        return 0
    fi

    nohup "${X11VNC_BIN}" \
        -display "${CHROME_DISPLAY}" \
        -rfbport "${VNC_PORT}" \
        -localhost \
        -forever \
        -shared \
        -nopw \
        -noxdamage \
        >"${X11VNC_LOG}" 2>&1 &
    printf '%s\n' "$!" >"${X11VNC_PID_FILE}"
    sleep 1
    pid_running "${X11VNC_PID_FILE}" || die "x11vnc failed to start"
}

start_websockify() {
    local assets_dir="${1:-}"
    [[ -n "${assets_dir}" ]] || return 0

    if pid_running "${WEBSOCKIFY_PID_FILE}"; then
        return 0
    fi

    nohup "${WEBSOCKIFY_BIN}" \
        --web "${WEB_ROOT}" \
        "${BIND_HOST}:${NOVNC_PORT}" \
        "127.0.0.1:${VNC_PORT}" \
        >"${WEBSOCKIFY_LOG}" 2>&1 &
    printf '%s\n' "$!" >"${WEBSOCKIFY_PID_FILE}"
    sleep 1
    pid_running "${WEBSOCKIFY_PID_FILE}" || die "websockify failed to start"
}

stop_pid() {
    local pid_file="${1:-}" pid=""
    [[ -f "${pid_file}" ]] || return 0
    pid="$(cat "${pid_file}" 2>/dev/null || true)"
    if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
        kill "${pid}" >/dev/null 2>&1 || true
        sleep 1
        kill -9 "${pid}" >/dev/null 2>&1 || true
    fi
    rm -f "${pid_file}"
}

print_status() {
    local assets_dir="${1:-}" novnc_url="unavailable" novnc_assets="missing" websockify_status="stopped"
    local x11vnc_status="stopped" overall_status="stopped"

    if pid_running "${X11VNC_PID_FILE}"; then
        x11vnc_status="running"
    fi

    if [[ -n "${assets_dir}" ]]; then
        novnc_assets="${assets_dir}"
        if pid_running "${WEBSOCKIFY_PID_FILE}"; then
            websockify_status="running"
            novnc_url="http://${BIND_HOST}:${NOVNC_PORT}"
        else
            websockify_status="stopped"
        fi
    fi

    if [[ "${x11vnc_status}" == "running" ]]; then
        if [[ -z "${assets_dir}" || "${websockify_status}" == "running" ]]; then
            overall_status="ready"
        else
            overall_status="degraded"
        fi
    fi

    cat <<EOF
status=${overall_status}
display=${CHROME_DISPLAY}
chrome_profile=${CHROME_PROFILE_DIR}
vnc_host=127.0.0.1
vnc_port=${VNC_PORT}
x11vnc_status=${x11vnc_status}
novnc_assets=${novnc_assets}
novnc_url=${novnc_url}
websockify_status=${websockify_status}
web_root=${WEB_ROOT}
EOF
}

cmd_start() {
    local assets_dir=""

    start_trusted_browser
    assets_dir="$(resolve_novnc_assets_dir || true)"
    ensure_web_root "${assets_dir}"
    start_x11vnc
    start_websockify "${assets_dir}"
    print_status "${assets_dir}"
}

cmd_status() {
    local assets_dir=""

    assets_dir="$(resolve_novnc_assets_dir || true)"
    ensure_web_root "${assets_dir}"
    print_status "${assets_dir}"
}

cmd_stop() {
    stop_pid "${WEBSOCKIFY_PID_FILE}"
    stop_pid "${X11VNC_PID_FILE}"
    cat <<EOF
status=stopped
display=${CHROME_DISPLAY}
chrome_profile=${CHROME_PROFILE_DIR}
EOF
}

main() {
    mkdir -p "${STATE_DIR}"

    case "${1:-}" in
        start)
            cmd_start
            ;;
        status)
            cmd_status
            ;;
        stop)
            cmd_stop
            ;;
        *)
            die "usage: $0 <start|status|stop>"
            ;;
    esac
}

main "$@"
