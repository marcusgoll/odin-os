#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BROWSER_ACCESS_SH="${SCRIPT_DIR}/../browser/browser-access.sh"
source "${BROWSER_ACCESS_SH}"

request_json="$(cat)"

tool_key="$(jq -r '.tool_key // "plaid_transfer_application"' <<<"${request_json}")"
action="$(jq -r '.input.action // "inspect"' <<<"${request_json}")"
application_url="$(jq -r '.input.application_url // "https://dashboard.plaid.com/transfer/application"' <<<"${request_json}")"
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

normalize_snapshot() {
    tr '[:upper:]' '[:lower:]' <<<"$1"
}

is_plaid_dashboard_url() {
    local target="${1:-}"
    [[ "${target}" =~ ^https://dashboard\.plaid\.com([/?#]|$) ]]
}

detect_state() {
    local snapshot
    snapshot="$(normalize_snapshot "$1")"

    if [[ "${snapshot}" == *"verification code"* || "${snapshot}" == *"authenticator"* || "${snapshot}" == *"two-factor"* || "${snapshot}" == *"mfa"* ]]; then
        printf '%s' "blocked_on_mfa"
        return 0
    fi
    if [[ "${snapshot}" == *"submitted for review"* || "${snapshot}" == *"under review"* ]]; then
        printf '%s' "submitted_for_review"
        return 0
    fi
    if [[ "${snapshot}" == *"already enabled"* || "${snapshot}" == *"transfer is enabled"* ]]; then
        printf '%s' "already_enabled"
        return 0
    fi
    if [[ "${snapshot}" == *"sign in"* || "${snapshot}" == *"log in"* || "${snapshot}" == *"login"* ]]; then
        printf '%s' "ready_for_login"
        return 0
    fi

    printf '%s' "unclassified"
}

json_result() {
    local status="$1" summary="$2" session_state="$3" current_url="$4" evidence="$5" screenshot_path="$6" next_action="$7"
    jq -nc \
        --arg status "${status}" \
        --arg tool_key "${tool_key}" \
        --arg summary "${summary}" \
        --arg session_state "${session_state}" \
        --arg current_url "${current_url}" \
        --arg evidence "${evidence}" \
        --arg screenshot_path "${screenshot_path}" \
        --arg next_action "${next_action}" \
        '{status: $status, tool_key: $tool_key, summary: $summary, session_state: $session_state, current_url: $current_url, screenshot_path: $screenshot_path, evidence: $evidence, next_action: $next_action, artifacts: {session_state: $session_state, current_url: $current_url, screenshot_path: $screenshot_path, evidence: $evidence, next_action: $next_action}}'
}

if ! is_plaid_dashboard_url "${application_url}"; then
    json_result "failed" "Plaid transfer application URL must be on Plaid Dashboard" "failed" "${application_url}" "" "" "unsupported application URL"
    exit 0
fi

if ! browser_request_domain_access "${application_url}"; then
    json_result "failed" "Plaid transfer application URL must be on Plaid Dashboard" "failed" "${application_url}" "" "" "unsupported application URL"
    exit 0
fi
if ! browser_server_start --url "${application_url}" --headless; then
    browser_server_stop >/dev/null 2>&1 || true
    json_result "failed" "Plaid transfer browser launch failed" "failed" "${application_url}" "" "" "launch_failed"
    exit 0
fi
evidence="$(browser_snapshot 2>/dev/null || true)"
session_state="$(detect_state "${evidence}")"

screenshot_path="${output_path}"
if [[ -z "${screenshot_path}" ]]; then
    screenshot_path="${ODIN_DIR:-${SCRIPT_DIR}/../../.odin-browser}/browser-state/plaid-transfer.png"
fi
if ! screenshot_path="$(browser_bc_screenshot --output "${screenshot_path}")"; then
    browser_server_stop >/dev/null 2>&1 || true
    json_result "failed" "Plaid transfer screenshot failed" "${session_state}" "${application_url}" "${evidence}" "" "screenshot_failed"
    exit 0
fi

case "${session_state}" in
    ready_for_login)
        next_action="sign in to Plaid"
        summary="Plaid transfer workflow requires login"
        ;;
    blocked_on_mfa)
        next_action="complete MFA challenge"
        summary="Plaid transfer workflow is blocked on MFA"
        ;;
    submitted_for_review)
        next_action="wait for review"
        summary="Plaid transfer application is under review"
        ;;
    already_enabled)
        next_action="no action needed"
        summary="Plaid transfer application is already enabled"
        ;;
    unclassified)
        next_action="inspect dashboard"
        summary="Plaid transfer workflow is unclassified"
        ;;
esac

browser_server_stop
json_result "completed" "${summary}" "${session_state}" "${application_url}" "${evidence}" "${screenshot_path}" "${next_action}"
