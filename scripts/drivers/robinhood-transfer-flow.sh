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
fixture_state="${ODIN_ROBINHOOD_TRANSFER_FIXTURE_STATE:-}"
attended_timeout_seconds="${ODIN_ROBINHOOD_TRANSFER_ATTENDED_TIMEOUT_SECONDS:-300}"
attended_poll_interval_seconds="${ODIN_ROBINHOOD_TRANSFER_ATTENDED_POLL_INTERVAL_SECONDS:-2}"
review_ready_patterns="${ODIN_ROBINHOOD_TRANSFER_REVIEW_READY_PATTERNS:-review your transfer;;review and confirm;;confirm transfer;;transfer review ready;;will be deducted from your bank account;;transfer \$}"
submitted_patterns="${ODIN_ROBINHOOD_TRANSFER_SUBMITTED_PATTERNS:-transfer submitted;;request submitted;;request received;;transfer complete;;deposit initiated}"
session_patterns="${ODIN_ROBINHOOD_TRANSFER_SESSION_PATTERNS:-sign in;;log in;;login;;verification code;;enter the code;;two-factor;;2fa;;authenticate}"

browser_started=0
prepare_outcome_state=""
prepare_outcome_evidence=""
submit_outcome_state=""
submit_outcome_evidence=""
submit_outcome_prior_session_state=""

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

cleanup() {
    if [[ "${browser_started}" == "1" ]]; then
        browser_server_stop >/dev/null 2>&1 || true
    fi
}

trap cleanup EXIT

lower_text() {
    local text="${1:-}"
    printf '%s' "${text,,}"
}

normalize_match_text() {
    local text="${1:-}"
    printf '%s' "${text}" | tr '\r\n\t' '   ' | sed 's/  */ /g;s/^ //;s/ $//'
}

should_preserve_external_robinhood_page() {
    local current_url="${1:-}"

    [[ -n "${fixture_state}" ]] && return 1
    [[ -n "${ODIN_BROWSER_SERVER_URL:-}" ]] || return 1
    [[ -n "${current_url}" ]] || return 1
    [[ "$(lower_text "${current_url}")" == *"robinhood.com"* ]]
}

matches_any_pattern() {
    local haystack patterns candidate
    haystack="$(lower_text "${1:-}")"
    patterns="${2:-}"
    [[ -n "${haystack}" ]] || return 1
    haystack="$(normalize_match_text "${haystack}")"
    [[ -n "${haystack}" ]] || return 1
    while IFS= read -r candidate || [[ -n "${candidate}" ]]; do
        candidate="${candidate//[$'\r']/}"
        [[ -n "${candidate}" ]] || continue
        candidate="$(normalize_match_text "${candidate}")"
        [[ -n "${candidate}" ]] || continue
        if [[ "${haystack}" == *"${candidate}"* ]]; then
            return 0
        fi
    done < <(printf '%s\n' "${patterns}" | tr ';' '\n' | sed '/^$/d')
    return 1
}

snapshot_is_review_ready() {
    matches_any_pattern "${1:-}" "${review_ready_patterns}"
}

snapshot_is_submitted() {
    matches_any_pattern "${1:-}" "${submitted_patterns}"
}

snapshot_is_session_challenge() {
    matches_any_pattern "${1:-}" "${session_patterns}"
}

prepare_launch_args() {
    if [[ -n "${fixture_state}" ]]; then
        printf '%s\n' "--headless"
        return 0
    fi
    printf '%s\n' "--headed"
}

wait_for_prepare_review() {
    local deadline now snapshot=""
    deadline=$(( $(date +%s) + attended_timeout_seconds ))

    while true; do
        snapshot="$(browser_snapshot 2>/dev/null || true)"
        if snapshot_is_review_ready "${snapshot}"; then
            prepare_outcome_state="review_ready"
            prepare_outcome_evidence="${snapshot}"
            return 0
        fi

        now="$(date +%s)"
        if (( now >= deadline )); then
            prepare_outcome_state=""
            prepare_outcome_evidence="${snapshot}"
            return 0
        fi

        sleep "${attended_poll_interval_seconds}"
    done
}

wait_for_submit_outcome() {
    local deadline now snapshot="" saw_session_expired="false"
    deadline=$(( $(date +%s) + attended_timeout_seconds ))

    while true; do
        snapshot="$(browser_snapshot 2>/dev/null || true)"
        if snapshot_is_submitted "${snapshot}"; then
            submit_outcome_state="submitted"
            submit_outcome_evidence="${snapshot}"
            submit_outcome_prior_session_state=""
            return 0
        fi
        if snapshot_is_session_challenge "${snapshot}"; then
            saw_session_expired="true"
        fi

        now="$(date +%s)"
        if (( now >= deadline )); then
            if snapshot_is_session_challenge "${snapshot}"; then
                submit_outcome_state="session_expired"
                submit_outcome_evidence="${snapshot}"
                submit_outcome_prior_session_state=""
                return 0
            fi
            if [[ "${saw_session_expired}" == "true" ]]; then
                submit_outcome_state="resume_verification_failed"
                submit_outcome_evidence="${snapshot}"
                submit_outcome_prior_session_state="session_expired"
                return 0
            fi
            submit_outcome_state="resume_verification_failed"
            submit_outcome_evidence="${snapshot}"
            submit_outcome_prior_session_state=""
            return 0
        fi

        sleep "${attended_poll_interval_seconds}"
    done
}

if ! browser_request_domain_access "${target_url}"; then
    json_result "failed" "Robinhood transfer domain access was rejected" "failed" "${target_url}" "" "retry driver setup" ""
    exit 0
fi
if ! browser_server_start --url "${target_url}" "$(prepare_launch_args)"; then
    json_result "failed" "Robinhood transfer browser launch failed" "failed" "${target_url}" "" "retry driver setup" ""
    exit 0
fi
browser_started=1
current_url="$(browser_current_url 2>/dev/null || true)"
if ! should_preserve_external_robinhood_page "${current_url}"; then
    if ! browser_navigate "${target_url}"; then
        json_result "failed" "Robinhood transfer navigation failed" "failed" "${target_url}" "" "retry driver setup" ""
        exit 0
    fi
fi

screenshot_path="${output_path}"
if [[ -z "${screenshot_path}" ]]; then
    screenshot_path="${ODIN_DIR:-${SCRIPT_DIR}/../../.odin-browser}/browser-state/robinhood-transfer.png"
fi
prior_session_state="${ODIN_ROBINHOOD_TRANSFER_PRIOR_SESSION_STATE:-}"
session_state="${fixture_state}"
evidence=""

if [[ -z "${session_state}" ]]; then
    if [[ "${mode}" == "submit" ]]; then
        wait_for_submit_outcome
        session_state="${submit_outcome_state:-resume_verification_failed}"
        evidence="${submit_outcome_evidence:-}"
        if [[ -z "${prior_session_state}" ]]; then
            prior_session_state="${submit_outcome_prior_session_state:-}"
        fi
    else
        wait_for_prepare_review
        session_state="${prepare_outcome_state:-}"
        evidence="${prepare_outcome_evidence:-}"
        if [[ "${session_state}" != "review_ready" ]]; then
            if ! screenshot_path="$(browser_bc_screenshot --output "${screenshot_path}")"; then
                screenshot_path=""
            fi
            json_result "failed" "Robinhood transfer review was not ready before attended timeout" "failed" "${target_url}" "${screenshot_path}" "complete attended review and rerun prepare" "${evidence}" ""
            exit 0
        fi
    fi
fi

if [[ -z "${evidence}" ]]; then
    evidence="$(browser_snapshot 2>/dev/null || true)"
fi
if ! screenshot_path="$(browser_bc_screenshot --output "${screenshot_path}")"; then
    screenshot_path=""
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

json_result "completed" "${summary}" "${session_state}" "${target_url}" "${screenshot_path}" "${next_action}" "${evidence}" "${prior_session_state}"
