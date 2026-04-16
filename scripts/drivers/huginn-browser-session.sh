#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BROWSER_ACCESS_SH="${SCRIPT_DIR}/../browser/browser-access.sh"
source "${BROWSER_ACCESS_SH}"

request_json="$(cat)"

tool_key="$(jq -r '.tool_key // "huginn_browser_session"' <<<"${request_json}")"
action="$(jq -r '.input.action // "health"' <<<"${request_json}")"
url="$(jq -r '.input.url // "https://example.com"' <<<"${request_json}")"
output_path="$(jq -r '.input.path // empty' <<<"${request_json}")"

host_from_url() {
    local target="$1" host
    host="${target#*://}"
    if [[ "${host}" == "${target}" ]]; then
        host="${target}"
    fi
    host="${host%%[/?#]*}"
    host="${host##*@}"
    host="${host%%:*}"
    printf '%s' "${host}"
}

json_result() {
    local status="$1" summary="$2" session_state="$3" current_url="$4" snapshot="$5" screenshot_path="$6" health_state="$7"
    jq -nc \
        --arg status "${status}" \
        --arg tool_key "${tool_key}" \
        --arg summary "${summary}" \
        --arg session_state "${session_state}" \
        --arg current_url "${current_url}" \
        --arg snapshot "${snapshot}" \
        --arg screenshot_path "${screenshot_path}" \
        --arg health_state "${health_state}" \
        '{status: $status, tool_key: $tool_key, summary: $summary, artifacts: {session_state: $session_state, current_url: $current_url, snapshot: $snapshot, screenshot_path: $screenshot_path, health_state: $health_state}}'
}

case "${action}" in
    health)
        health_payload="$(browser_server_health 2>/dev/null || true)"
        if jq -e . >/dev/null 2>&1 <<<"${health_payload}"; then
            health_state="$(jq -r 'if .browser == true then "healthy" else (.state // "stopped") end' <<<"${health_payload}")"
        else
            health_state="${health_payload:-${ODIN_BROWSER_STUB_HEALTH_STATE:-healthy}}"
        fi
        json_result "completed" "browser session health checked" "${health_state}" "" "" "" "${health_state}"
        ;;
    launch)
        browser_request_domain_access "$(host_from_url "${url}")"
        browser_server_start --url "${url}" --headless
        browser_navigate "${url}"
        snapshot="$(browser_snapshot 2>/dev/null || true)"
        json_result "completed" "browser session launched" "running" "${url}" "${snapshot}" "" ""
        ;;
    snapshot)
        snapshot="$(browser_snapshot 2>/dev/null || true)"
        json_result "completed" "browser snapshot captured" "running" "${url}" "${snapshot}" "" ""
        ;;
    screenshot)
        if [[ -z "${output_path}" ]]; then
            output_path="${ODIN_DIR:-${SCRIPT_DIR}/../../.odin-browser}/browser-state/browser.png"
        fi
        screenshot_path="$(browser_bc_screenshot --output "${output_path}" 2>/dev/null || true)"
        json_result "completed" "browser screenshot captured" "running" "${url}" "" "${screenshot_path}" ""
        ;;
    stop)
        browser_server_stop
        json_result "completed" "browser session stopped" "stopped" "" "" "" ""
        ;;
    *)
        json_result "completed" "unknown browser session action: ${action}" "failed" "${url}" "" "" ""
        ;;
esac
