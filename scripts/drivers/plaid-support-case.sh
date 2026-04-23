#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_BROWSER_ACCESS_SH="${SCRIPT_DIR}/../browser/browser-access.sh"

resolve_browser_access_lib() {
    if [[ -n "${ODIN_BROWSER_ACCESS_LIB_PATH:-}" ]]; then
        printf '%s\n' "${ODIN_BROWSER_ACCESS_LIB_PATH}"
        return 0
    fi
    printf '%s\n' "${DEFAULT_BROWSER_ACCESS_SH}"
}

emit_json() {
    local status="$1" tool_key="$2" summary="$3" artifacts_json=""
    artifacts_json="$(cat)"
    local artifacts_b64=""
    artifacts_b64="$(printf '%s' "${artifacts_json}" | base64 | tr -d '\n')"
    ARTIFACTS_JSON_B64="${artifacts_b64}" python3 - "${status}" "${tool_key}" "${summary}" <<'PY'
import base64
import json
import os
import sys

status, tool_key, summary = sys.argv[1:4]
artifacts_raw = ""
try:
    artifacts_raw = base64.b64decode(os.environ.get("ARTIFACTS_JSON_B64", "")).decode("utf-8")
    artifacts = json.loads(artifacts_raw)
except Exception:
    artifacts = {}

print(json.dumps({
    "status": status,
    "tool_key": tool_key,
    "summary": summary,
    "artifacts": artifacts,
}))
PY
}

request_json="$(cat)"
tool_key="$(jq -r '.tool_key // "plaid_support_case"' <<<"${request_json}")"
support_url="$(jq -r '.input.support_url // "https://dashboard.plaid.com/support/new/admin/account-administration"' <<<"${request_json}")"
category="$(jq -r '.input.category // "Plaid pricing and billing"' <<<"${request_json}")"
subject="$(jq -r '.input.subject // ""' <<<"${request_json}")"
country="$(jq -r '.input.country // ""' <<<"${request_json}")"
body="$(jq -r '.input.body // ""' <<<"${request_json}")"
submit_requested="$(jq -r '.input.submit // false' <<<"${request_json}" | tr '[:upper:]' '[:lower:]')"

browser_access_sh="$(resolve_browser_access_lib)"
if [[ ! -f "${browser_access_sh}" ]]; then
    jq -nc \
        --arg session_state "browser_lib_missing" \
        --arg browser_access_path "${browser_access_sh}" \
        '{session_state: $session_state, browser_access_path: $browser_access_path}' | emit_json "failed" "${tool_key}" "Browser access library not found."
    exit 0
fi

# shellcheck source=/dev/null
source "${browser_access_sh}"

cleanup() {
    browser_server_stop >/dev/null 2>&1 || true
}
trap cleanup EXIT

page_state_json() {
    local raw="null"
    raw="$(browser_evaluate '(() => ({ url: location.href, title: document.title, hasSubject: !!document.querySelector("#subject"), hasCountry: !!document.querySelector("#countryCode-input"), hasBody: !!document.querySelector("#body"), buttons: [...document.querySelectorAll("button")].map(el => (el.textContent || "").trim()).filter(Boolean).slice(0, 20), text: document.body.innerText.slice(0, 2000) }))()' 2>/dev/null || printf 'null')"
    jq -nc --arg raw "${raw}" '(($raw | fromjson? // null)) as $parsed | if ($parsed | type) == "object" then $parsed else {raw: $parsed} end'
}

normalize_snapshot() {
    tr '[:upper:]' '[:lower:]' <<<"$1"
}

is_javascript_shell() {
    local snapshot
    snapshot="$(normalize_snapshot "$1")"
    [[ "${snapshot}" == *"enable javascript to run this app"* ]]
}

snapshot_has_pricing_form() {
    local snapshot
    snapshot="$(normalize_snapshot "$1")"
    [[ "${snapshot}" == *"case details"* ]] || [[ "${snapshot}" == *"subject"* ]]
}

snapshot_is_transfer_upgrade_page() {
    local snapshot
    snapshot="$(normalize_snapshot "$1")"
    [[ "${snapshot}" == *"all products"* ]] && [[ "${snapshot}" == *"transfer"* ]] && [[ "${snapshot}" == *"upgrade plan"* ]]
}

continue_to_case() {
    browser_evaluate '(() => { const btn = [...document.querySelectorAll("button")].find(el => /Continue to open a case/i.test(el.textContent || "")); if (!btn) return { ok: false, reason: "continue_not_found" }; btn.click(); return { ok: true }; })()' 2>/dev/null || printf 'null'
}

click_contact_support() {
    browser_evaluate '(() => { const btn = [...document.querySelectorAll("button")].find(el => /Contact Plaid Support/i.test(el.textContent || "")); if (!btn) return { ok: false, reason: "submit_not_found" }; btn.click(); return { ok: true }; })()' 2>/dev/null || printf 'null'
}

wait_for_pricing_form() {
    local attempts="${1:-15}" ready=""
    for _ in $(seq 1 "${attempts}"); do
        ready="$(browser_evaluate '(() => !!document.querySelector("#subject"))()' 2>/dev/null || printf 'false')"
        if [[ "${ready}" == "true" ]]; then
            return 0
        fi
        sleep 1
    done
    return 1
}

wait_for_post_question_type_state() {
    local attempts="${1:-20}" snapshot=""
    for _ in $(seq 1 "${attempts}"); do
        snapshot="$(browser_snapshot 2>/dev/null || true)"
        if snapshot_has_pricing_form "${snapshot}"; then
            printf 'pricing_case_form_ready'
            return 0
        fi
        if snapshot_is_transfer_upgrade_page "${snapshot}"; then
            printf 'transfer_upgrade_required'
            return 0
        fi
        sleep 1
    done
    printf 'timeout'
    return 0
}

wait_for_hydrated_page() {
    local attempts="${1:-15}" snapshot=""
    for _ in $(seq 1 "${attempts}"); do
        snapshot="$(browser_snapshot 2>/dev/null || true)"
        if [[ -n "${snapshot}" ]] && ! is_javascript_shell "${snapshot}"; then
            return 0
        fi
        sleep 1
    done
    return 1
}

if ! declare -F browser_trusted_session_start >/dev/null 2>&1 || \
   ! declare -F browser_click_selector >/dev/null 2>&1 || \
   ! declare -F browser_type_selector >/dev/null 2>&1 || \
   ! declare -F browser_evaluate >/dev/null 2>&1 || \
   ! declare -F browser_snapshot >/dev/null 2>&1; then
    jq -nc \
        --arg session_state "browser_helpers_missing" \
        '{session_state: $session_state}' | emit_json "failed" "${tool_key}" "Required browser helpers are unavailable."
    exit 0
fi

if ! browser_trusted_session_start --url "${support_url}" >/dev/null 2>&1; then
    jq -nc \
        --arg session_state "browser_start_failed" \
        --arg support_url "${support_url}" \
        '{session_state: $session_state, support_url: $support_url}' | emit_json "failed" "${tool_key}" "Unable to start Plaid support browser session."
    exit 0
fi

if declare -F browser_navigate >/dev/null 2>&1; then
    browser_navigate "${support_url}" >/dev/null 2>&1 || true
fi

if ! wait_for_hydrated_page 20; then
    jq -nc \
        --arg session_state "javascript_shell_not_ready" \
        --arg snapshot "$(browser_snapshot 2>/dev/null || true)" \
        --arg page "$(page_state_json)" \
        '{session_state: $session_state, snapshot: $snapshot, page: ($page | fromjson? // null)}' | emit_json "failed" "${tool_key}" "Plaid support page never hydrated past the JavaScript shell."
    exit 0
fi

continue_result='{"ok":true,"skipped":true}'
if ! wait_for_pricing_form 1; then
    continue_result="$(continue_to_case)"
    if ! jq -e '.ok == true' <<<"${continue_result}" >/dev/null 2>&1; then
        jq -nc \
            --arg session_state "continue_to_case_failed" \
            --arg continue_result "${continue_result}" \
            --arg page "$(page_state_json)" \
            '{session_state: $session_state, continue_result: ($continue_result | fromjson? // null), page: ($page | fromjson? // null)}' | emit_json "failed" "${tool_key}" "Unable to open the Plaid support case form."
        exit 0
    fi

    browser_click_selector '#secondaryCategoryDropdown .css-1012nnc-control' >/dev/null 2>&1 || true

    if ! browser_type_selector '#secondaryCategoryDropdown-input' "${category}" --submit >/dev/null 2>&1; then
        jq -nc \
            --arg session_state "question_type_select_failed" \
            --arg category "${category}" \
            --arg page "$(page_state_json)" \
            '{session_state: $session_state, category: $category, page: ($page | fromjson? // null)}' | emit_json "failed" "${tool_key}" "Unable to select the Plaid support question type."
        exit 0
    fi

    post_question_type_state="$(wait_for_post_question_type_state 20)"
    if [[ "${post_question_type_state}" == "transfer_upgrade_required" ]]; then
        jq -nc \
            --arg session_state "transfer_upgrade_required" \
            --arg category "${category}" \
            --arg page "$(page_state_json)" \
            '{session_state: $session_state, category: $category, current_url: ((($page | fromjson? // {}) | .url) // ""), page: ($page | fromjson? // null)}' | emit_json "completed" "${tool_key}" "Plaid redirected Transfer access back to the products catalog, where Transfer is still marked Upgrade plan."
        exit 0
    fi
    if [[ "${post_question_type_state}" != "pricing_case_form_ready" ]] && ! wait_for_pricing_form 1; then
        jq -nc \
            --arg session_state "pricing_form_not_ready" \
            --arg category "${category}" \
            --arg page "$(page_state_json)" \
            '{session_state: $session_state, category: $category, page: ($page | fromjson? // null)}' | emit_json "failed" "${tool_key}" "Plaid support pricing form did not become ready."
        exit 0
    fi
fi

if [[ -n "${subject}" ]] && ! browser_type_selector '#subject' "${subject}" >/dev/null 2>&1; then
    jq -nc \
        --arg session_state "subject_fill_failed" \
        --arg page "$(page_state_json)" \
        '{session_state: $session_state, page: ($page | fromjson? // null)}' | emit_json "failed" "${tool_key}" "Unable to fill the Plaid support subject."
    exit 0
fi

if [[ -n "${country}" ]] && ! browser_type_selector '#countryCode-input' "${country}" --submit >/dev/null 2>&1; then
    jq -nc \
        --arg session_state "country_fill_failed" \
        --arg page "$(page_state_json)" \
        '{session_state: $session_state, page: ($page | fromjson? // null)}' | emit_json "failed" "${tool_key}" "Unable to fill the Plaid support country."
    exit 0
fi

if [[ -n "${body}" ]] && ! browser_type_selector '#body' "${body}" >/dev/null 2>&1; then
    jq -nc \
        --arg session_state "body_fill_failed" \
        --arg page "$(page_state_json)" \
        '{session_state: $session_state, page: ($page | fromjson? // null)}' | emit_json "failed" "${tool_key}" "Unable to fill the Plaid support description."
    exit 0
fi

if [[ "${submit_requested}" == "true" ]]; then
    submit_result="$(click_contact_support)"
    if ! jq -e '.ok == true' <<<"${submit_result}" >/dev/null 2>&1; then
        jq -nc \
            --arg session_state "submit_click_failed" \
            --arg submit_result "${submit_result}" \
            --arg page "$(page_state_json)" \
            '{session_state: $session_state, submit_result: ($submit_result | fromjson? // null), page: ($page | fromjson? // null)}' | emit_json "failed" "${tool_key}" "Unable to click Contact Plaid Support."
        exit 0
    fi
    sleep 5
    jq -nc \
        --arg session_state "pricing_case_submission_attempted" \
        --arg category "${category}" \
        --arg continue_result "${continue_result}" \
        --arg submit_result "${submit_result}" \
        --arg page "$(page_state_json)" \
        '{session_state: $session_state, category: $category, continue_result: ($continue_result | fromjson? // null), submit_result: ($submit_result | fromjson? // null), current_url: ((($page | fromjson? // {}) | .url) // ""), page: ($page | fromjson? // null)}' | emit_json "completed" "${tool_key}" "Plaid support case submission was attempted."
    exit 0
fi

jq -nc \
    --arg session_state "pricing_case_form_ready" \
    --arg category "${category}" \
    --arg continue_result "${continue_result}" \
    --arg page "$(page_state_json)" \
    '{session_state: $session_state, category: $category, continue_result: ($continue_result | fromjson? // null), current_url: ((($page | fromjson? // {}) | .url) // ""), page: ($page | fromjson? // null)}' | emit_json "completed" "${tool_key}" "Plaid support pricing form is ready."
