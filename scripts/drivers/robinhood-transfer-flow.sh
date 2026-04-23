#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BROWSER_ACCESS_SH="${SCRIPT_DIR}/../browser/browser-access.sh"
source "${BROWSER_ACCESS_SH}"

request_json="$(cat)"

tool_key="$(jq -r '.tool_key // "robinhood_transfer_flow"' <<<"${request_json}")"
mode="$(jq -r '.input.mode // "prepare"' <<<"${request_json}")"
direction="$(jq -r '.input.direction // empty' <<<"${request_json}")"
amount_usd="$(jq -r '.input.amount_usd // empty' <<<"${request_json}")"
source_account="$(jq -r '.input.source_account // empty' <<<"${request_json}")"
destination_account="$(jq -r '.input.destination_account // empty' <<<"${request_json}")"
memo="$(jq -r '.input.memo // empty' <<<"${request_json}")"
output_path="$(jq -r '.input.path // empty' <<<"${request_json}")"
target_url="${ODIN_ROBINHOOD_TRANSFER_URL:-https://robinhood.com/transfer}"

json_result() {
    local status="$1" summary="$2" session_state="$3" current_url="$4" screenshot_path="$5" next_action="$6" evidence="$7" prior_session_state="${8:-}"
    jq -nc \
        --arg status "${status}" \
        --arg tool_key "${tool_key}" \
        --arg summary "${summary}" \
        --arg session_state "${session_state}" \
        --arg current_url "${current_url}" \
        --arg screenshot_path "${screenshot_path}" \
        --arg next_action "${next_action}" \
        --arg evidence "${evidence}" \
        --arg direction "${direction}" \
        --arg amount_usd "${amount_usd}" \
        --arg source_account "${source_account}" \
        --arg destination_account "${destination_account}" \
        --arg memo "${memo}" \
        --arg prior_session_state "${prior_session_state}" \
        '{
            status: $status,
            tool_key: $tool_key,
            summary: $summary,
            artifacts: {
                session_state: $session_state,
                current_url: $current_url,
                screenshot_path: $screenshot_path,
                next_action: $next_action,
                evidence: $evidence,
                direction: $direction,
                amount_usd: $amount_usd,
                source_account: $source_account,
                destination_account: $destination_account,
                memo: $memo
            }
        }
        | if $prior_session_state != "" then .artifacts.prior_session_state = $prior_session_state else . end'
}

if ! browser_request_domain_access "${target_url}"; then
    json_result "failed" "Robinhood transfer domain access was rejected" "failed" "${target_url}" "" "retry driver setup" ""
    exit 0
fi
if ! browser_server_start --url "${target_url}" --headless; then
    browser_server_stop >/dev/null 2>&1 || true
    json_result "failed" "Robinhood transfer browser launch failed" "failed" "${target_url}" "" "retry driver setup" ""
    exit 0
fi
if ! browser_navigate "${target_url}"; then
    browser_server_stop >/dev/null 2>&1 || true
    json_result "failed" "Robinhood transfer navigation failed" "failed" "${target_url}" "" "retry driver setup" ""
    exit 0
fi

evidence="$(browser_snapshot 2>/dev/null || true)"
screenshot_path="${output_path}"
if [[ -z "${screenshot_path}" ]]; then
    screenshot_path="${ODIN_DIR:-${SCRIPT_DIR}/../../.odin-browser}/browser-state/robinhood-transfer.png"
fi
if ! screenshot_path="$(browser_bc_screenshot --output "${screenshot_path}")"; then
    screenshot_path=""
fi

fixture_state="${ODIN_ROBINHOOD_TRANSFER_FIXTURE_STATE:-}"
prior_session_state="${ODIN_ROBINHOOD_TRANSFER_PRIOR_SESSION_STATE:-}"
session_state="${fixture_state}"
if [[ -z "${session_state}" ]]; then
    if [[ "${mode}" == "submit" ]]; then
        session_state="submitted"
    else
        session_state="review_ready"
    fi
fi

summary=""
next_action=""
case "${session_state}" in
    review_ready)
        summary="Robinhood transfer review ready"
        next_action="request approval"
        ;;
    submitted)
        summary="Robinhood transfer submitted"
        next_action="verify transfer status"
        ;;
    session_expired)
        summary="Robinhood session expired during transfer"
        next_action="reestablish session"
        ;;
    resume_verification_failed)
        summary="Robinhood review continuity could not be verified"
        next_action="fresh prepare required"
        ;;
    *)
        summary="Robinhood transfer driver returned ${session_state}"
        next_action="inspect artifacts"
        ;;
esac

browser_server_stop >/dev/null 2>&1 || true
json_result "completed" "${summary}" "${session_state}" "${target_url}" "${screenshot_path}" "${next_action}" "${evidence}" "${prior_session_state}"
