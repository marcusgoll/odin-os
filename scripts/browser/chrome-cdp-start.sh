#!/usr/bin/env bash
# Minimal repo-local real Chrome CDP starter for trusted headed sessions.

[[ "${BASH_SOURCE[0]}" == "${0}" ]] && set -euo pipefail
[[ -n "${_ODIN_CHROME_CDP_LOADED:-}" ]] && return 0
_ODIN_CHROME_CDP_LOADED=1

ODIN_DIR="${ODIN_DIR:-${ODIN_ROOT:-/var/odin}}"
CHROME_CDP_PORT="${ODIN_CHROME_CDP_PORT:-9222}"
CHROME_DISPLAY="${ODIN_CHROME_DISPLAY:-:99}"
CHROME_PROFILE="${ODIN_CHROME_PROFILE_DIR:-${ODIN_DIR}/browser-state/chrome-profile}"
CHROME_PID_FILE="${ODIN_DIR}/browser-state/chrome-cdp.pid"
XVFB_PID_FILE="${ODIN_DIR}/browser-state/xvfb.pid"
CHROME_LOG_FILE="${ODIN_DIR}/logs/$(date +%Y-%m-%d)/chrome-cdp.log"
ODIN_CHROME_CDP_OWNED=0
ODIN_CHROME_XVFB_OWNED=0

chrome_cdp_log() {
    local log_dir="${ODIN_DIR}/logs/$(date +%Y-%m-%d)"
    mkdir -p "${log_dir}" 2>/dev/null || true
    printf '[chrome-cdp] %s %s\n' "$(date -Iseconds)" "$*" >> "${log_dir}/alerts.log" 2>/dev/null || true
}

chrome_profile_in_use() {
    pgrep -f -- "--user-data-dir=${CHROME_PROFILE}" >/dev/null 2>&1
}

existing_profile_cdp_port() {
    local match=""

    match="$(pgrep -af -- "--user-data-dir=${CHROME_PROFILE}" 2>/dev/null | grep -oE -- '--remote-debugging-port=[0-9]+' | tail -n 1 || true)"
    if [[ -z "${match}" ]]; then
        return 1
    fi
    printf '%s\n' "${match#--remote-debugging-port=}"
}

clear_stale_chrome_singletons() {
    local singleton_paths=(
        "${CHROME_PROFILE}/SingletonLock"
        "${CHROME_PROFILE}/SingletonCookie"
        "${CHROME_PROFILE}/SingletonSocket"
    )
    local found=0 path=""

    for path in "${singleton_paths[@]}"; do
        if [[ -e "${path}" || -L "${path}" ]]; then
            found=1
            break
        fi
    done

    if [[ "${found}" != "1" ]]; then
        return 0
    fi
    if chrome_profile_in_use; then
        return 0
    fi

    rm -f "${singleton_paths[@]}"
    chrome_cdp_log "Cleared stale Chrome singleton files for ${CHROME_PROFILE}"
}

find_chrome_cdp_bin() {
    command -v "${CHROME_BIN:-google-chrome}" 2>/dev/null || \
    command -v google-chrome-stable 2>/dev/null || \
    command -v google-chrome 2>/dev/null || \
    command -v chromium 2>/dev/null || \
    command -v chromium-browser 2>/dev/null
}

cdp_url() {
    printf 'http://127.0.0.1:%s\n' "${CHROME_CDP_PORT}"
}

cdp_is_ready() {
    curl -sf "$(cdp_url)/json/version" >/dev/null 2>&1
}

cdp_port_available() {
    python3 - "${1:-}" <<'PY'
import socket
import sys

port = int(sys.argv[1])
sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
try:
    sock.bind(("127.0.0.1", port))
except OSError:
    raise SystemExit(1)
finally:
    sock.close()
PY
}

pick_free_cdp_port() {
    python3 - <<'PY'
import socket

sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()
PY
}

cdp_start() {
    local chrome_bin="" reused_port=""

    mkdir -p "${CHROME_PROFILE}" "${ODIN_DIR}/browser-state"
    clear_stale_chrome_singletons

    if cdp_is_ready; then
        ODIN_CHROME_CDP_OWNED=0
        return 0
    fi

    if reused_port="$(existing_profile_cdp_port 2>/dev/null || true)"; then
        if [[ -n "${reused_port}" ]]; then
            CHROME_CDP_PORT="${reused_port}"
            if cdp_is_ready; then
                ODIN_CHROME_CDP_OWNED=0
                chrome_cdp_log "Reusing existing Chrome CDP on port ${CHROME_CDP_PORT}"
                return 0
            fi
        fi
    fi

    if ! cdp_port_available "${CHROME_CDP_PORT}"; then
        CHROME_CDP_PORT="$(pick_free_cdp_port)" || return 1
        chrome_cdp_log "Configured Chrome CDP port busy; falling back to ${CHROME_CDP_PORT}"
    fi

    chrome_bin="$(find_chrome_cdp_bin)"
    [[ -n "${chrome_bin}" ]] || return 1

    if ! pgrep -f "Xvfb ${CHROME_DISPLAY}" >/dev/null 2>&1; then
        Xvfb "${CHROME_DISPLAY}" -screen 0 1920x1080x24 >/dev/null 2>&1 &
        printf '%s\n' "$!" > "${XVFB_PID_FILE}"
        ODIN_CHROME_XVFB_OWNED=1
        sleep 1
        chrome_cdp_log "Started Xvfb on ${CHROME_DISPLAY}"
    fi

    mkdir -p "$(dirname "${CHROME_LOG_FILE}")" 2>/dev/null || true

    DISPLAY="${CHROME_DISPLAY}" "${chrome_bin}" \
        --remote-debugging-port="${CHROME_CDP_PORT}" \
        --user-data-dir="${CHROME_PROFILE}" \
        --no-sandbox \
        --disable-dev-shm-usage \
        --no-first-run \
        --disable-default-apps \
        --disable-background-networking \
        --disable-sync \
        --disable-translate \
        --metrics-recording-only \
        --no-default-browser-check \
        about:blank >>"${CHROME_LOG_FILE}" 2>&1 &
    printf '%s\n' "$!" > "${CHROME_PID_FILE}"
    ODIN_CHROME_CDP_OWNED=1

    for _ in $(seq 1 40); do
        if cdp_is_ready; then
            chrome_cdp_log "Chrome CDP ready on port ${CHROME_CDP_PORT}"
            return 0
        fi
        sleep 0.25
    done

    cdp_stop >/dev/null 2>&1 || true
    return 1
}

cdp_stop() {
    local pid=""

    if [[ "${ODIN_CHROME_CDP_OWNED:-0}" == "1" && -f "${CHROME_PID_FILE}" ]]; then
        pid="$(cat "${CHROME_PID_FILE}" 2>/dev/null || true)"
        if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
            kill "${pid}" >/dev/null 2>&1 || true
            sleep 1
            kill -9 "${pid}" >/dev/null 2>&1 || true
        fi
        rm -f "${CHROME_PID_FILE}"
    fi

    if [[ "${ODIN_CHROME_XVFB_OWNED:-0}" == "1" && -f "${XVFB_PID_FILE}" ]]; then
        pid="$(cat "${XVFB_PID_FILE}" 2>/dev/null || true)"
        if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
            kill "${pid}" >/dev/null 2>&1 || true
            sleep 1
            kill -9 "${pid}" >/dev/null 2>&1 || true
        fi
        rm -f "${XVFB_PID_FILE}"
    fi

    ODIN_CHROME_CDP_OWNED=0
    ODIN_CHROME_XVFB_OWNED=0
    chrome_cdp_log "Stopped Chrome CDP and Xvfb"
}
