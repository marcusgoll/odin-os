#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_BROWSER_ACCESS_SH="${SCRIPT_DIR}/../browser/browser-access.sh"

emit_json() {
    local status="$1" summary="$2" artifacts_json="${3:-{}}"
    artifacts_json="$(json_object_or_empty "${artifacts_json}")"
    jq -nc \
        --arg status "${status}" \
        --arg summary "${summary}" \
        --argjson artifacts "${artifacts_json}" \
        '{status: $status, tool_key: "browser_x_profile_bio_update", summary: $summary, artifacts: $artifacts}'
}

json_object_or_empty() {
    local value="${1:-}"
    local normalized=""
    normalized="$(printf '%s' "${value}" | jq -c 'select(type == "object")' 2>/dev/null || true)"
    if [[ -n "${normalized}" ]]; then
        printf '%s\n' "${normalized}"
    else
        printf '{}\n'
    fi
}

request_json="$(cat)"

mapfile -t request_fields < <(REQUEST_JSON="${request_json}" python3 - <<'PY'
import json
import os
from urllib.parse import urlparse

try:
    request = json.loads(os.environ.get("REQUEST_JSON") or "{}")
except Exception as exc:
    print("error")
    print(f"invalid request json: {exc}")
    raise SystemExit(0)

payload = request.get("input") or {}
bio = str(payload.get("bio") or "").strip()
target_url = str(payload.get("target_url") or "https://x.com/settings/profile").strip()
profile_url = str(payload.get("profile_url") or "").strip()
label = str(payload.get("label") or "x-bio-update").strip() or "x-bio-update"
task_id = str(payload.get("task_id") or "").strip()
run_id = str(payload.get("run_id") or "").strip()
approval_id = str(payload.get("approval_id") or "").strip()

parsed = urlparse(target_url)
host = (parsed.hostname or "").strip().lower()
profile_parsed = urlparse(profile_url) if profile_url else None
profile_host = ((profile_parsed.hostname or "").strip().lower() if profile_parsed else "")
if not bio:
    print("error")
    print("bio is required")
elif len(bio) > 160:
    print("error")
    print("bio must be 160 characters or less")
elif parsed.scheme not in {"http", "https"} or host not in {"x.com", "www.x.com", "twitter.com", "www.twitter.com"}:
    print("error")
    print("target_url must be an X URL")
elif profile_url and (profile_parsed.scheme not in {"http", "https"} or profile_host not in {"x.com", "www.x.com", "twitter.com", "www.twitter.com"}):
    print("error")
    print("profile_url must be an X URL")
else:
    print("ok")
    print(bio)
    print(target_url)
    print(profile_url)
    print(label)
    print(task_id)
    print(run_id)
    print(approval_id)
PY
)

if [[ "${request_fields[0]:-error}" != "ok" ]]; then
    emit_json "failed" "${request_fields[1]:-invalid request}" '{"reason":"invalid_request"}'
    exit 0
fi

bio="${request_fields[1]}"
target_url="${request_fields[2]}"
profile_url="${request_fields[3]}"
label="${request_fields[4]}"
task_id="${request_fields[5]}"
run_id="${request_fields[6]}"
approval_id="${request_fields[7]}"

if [[ -n "${ODIN_TEST_X_BIO_DRIVER_RESULT:-}" ]]; then
    printf '%s\n' "${ODIN_TEST_X_BIO_DRIVER_RESULT}"
    exit 0
fi

browser_access_sh="${ODIN_BROWSER_ACCESS_LIB_PATH:-${DEFAULT_BROWSER_ACCESS_SH}}"
if [[ ! -f "${browser_access_sh}" ]]; then
    artifacts_json="$(jq -nc --arg reason "browser_lib_missing" --arg path "${browser_access_sh}" '{reason: $reason, browser_access_path: $path}')"
    emit_json "failed" "Browser access library not found." "${artifacts_json}"
    exit 0
fi

# shellcheck source=/dev/null
source "${browser_access_sh}"

browser_start_diagnostics() {
    local browser_state_dir="${BROWSER_STATE_DIR:-${ODIN_DIR:-${ODIN_ROOT:-/var/odin}}/browser-state}"
    local log_dir="${ODIN_DIR:-${ODIN_ROOT:-/var/odin}}/logs/$(date +%Y-%m-%d)"
    local browser_state_listing="" chrome_log_tail="" browser_log_tail="" alerts_log_tail=""

    browser_state_listing="$(ls -la "${browser_state_dir}" 2>&1 || true)"
    chrome_log_tail="$(tail -120 "${log_dir}/chrome-cdp.log" 2>&1 || true)"
    browser_log_tail="$(tail -80 "${log_dir}/browser-runtime.log" 2>&1 || true)"
    alerts_log_tail="$(tail -80 "${log_dir}/alerts.log" 2>&1 || true)"

    jq -nc \
        --arg browser_state_dir "${browser_state_dir}" \
        --arg browser_state_listing "${browser_state_listing}" \
        --arg chrome_log_tail "${chrome_log_tail}" \
        --arg browser_log_tail "${browser_log_tail}" \
        --arg alerts_log_tail "${alerts_log_tail}" \
        '{browser_state_dir: $browser_state_dir, browser_state_listing: $browser_state_listing, chrome_log_tail: $chrome_log_tail, browser_log_tail: $browser_log_tail, alerts_log_tail: $alerts_log_tail}'
}

cleanup() {
    browser_server_stop >/dev/null 2>&1 || true
}
trap cleanup EXIT

browser_started=false
for start_attempt in 1 2; do
    if browser_trusted_session_start --url "${target_url}" >/dev/null 2>&1; then
        browser_started=true
        break
    fi
    browser_server_stop >/dev/null 2>&1 || true
    sleep 1
done
if [[ "${browser_started}" != "true" ]]; then
    diagnostics="$(browser_start_diagnostics)"
    artifacts_json="$(jq -nc \
        --arg reason "browser_start_failed" \
        --arg target_url "${target_url}" \
        --arg profile_url "${profile_url}" \
        --arg task_id "${task_id}" \
        --arg run_id "${run_id}" \
        --arg approval_id "${approval_id}" \
        --argjson diagnostics "${diagnostics}" \
        '{reason: $reason, target_url: $target_url, profile_url: $profile_url, task_id: $task_id, run_id: $run_id, approval_id: $approval_id, browser_start_attempts: 2, diagnostics: $diagnostics, save_clicked: false, bio_verified: false}')"
    emit_json "failed" "Unable to start trusted X browser session." "${artifacts_json}"
    exit 0
fi

find_bio_eval='(() => {
  const visible = (node) => {
    if (!node) return false;
    const rect = node.getBoundingClientRect();
    const style = window.getComputedStyle(node);
    return !!(rect.width && rect.height) && style.visibility !== "hidden" && style.display !== "none";
  };
  const selectors = [
    "textarea[name=\"description\"]",
    "textarea[aria-label*=\"Bio\" i]",
    "div[role=\"textbox\"][aria-label*=\"Bio\" i]"
  ];
  for (const selector of selectors) {
    const node = Array.from(document.querySelectorAll(selector)).find(visible);
    if (node) {
      return {selector, current_url: location.href, title: document.title};
    }
  }
  return {selector: "", current_url: location.href, title: document.title};
})()'

bio_state='{}'
bio_selector=""
for _ in $(seq 1 30); do
    bio_state="$(json_object_or_empty "$(browser_evaluate "${find_bio_eval}" 2>/dev/null || true)")"
    bio_selector="$(jq -r '.selector // empty' <<<"${bio_state}")"
    if [[ -n "${bio_selector}" ]]; then
        break
    fi
    sleep 0.5
done

if [[ -z "${bio_selector}" ]]; then
    current_url="$(jq -r '.current_url // empty' <<<"${bio_state}" 2>/dev/null || true)"
    title="$(jq -r '.title // empty' <<<"${bio_state}" 2>/dev/null || true)"
    artifacts_json="$(jq -nc --arg reason "bio_field_missing" --arg current_url "${current_url}" --arg title "${title}" '{reason: $reason, current_url: $current_url, title: $title}')"
    emit_json "failed" "Unable to find the X profile bio field." "${artifacts_json}"
    exit 0
fi

set_bio_eval="$(python3 - "${bio_selector}" "${bio}" <<'PY'
import json
import sys

selector, bio = sys.argv[1:3]
print(f"""(() => {{
  const node = document.querySelector({json.dumps(selector)});
  if (!node) return {{ok: false, reason: "bio_field_missing", current_url: location.href, title: document.title}};
  node.focus();
  if ("value" in node) {{
    node.value = "";
    node.dispatchEvent(new Event("input", {{bubbles: true}}));
    node.value = {json.dumps(bio)};
    node.dispatchEvent(new Event("input", {{bubbles: true}}));
    node.dispatchEvent(new Event("change", {{bubbles: true}}));
  }} else {{
    node.textContent = {json.dumps(bio)};
    node.dispatchEvent(new InputEvent("input", {{bubbles: true, inputType: "insertText", data: {json.dumps(bio)}}}));
  }}
  return {{ok: true, current_url: location.href, title: document.title}};
}})()""")
PY
)"

set_state="$(json_object_or_empty "$(browser_evaluate "${set_bio_eval}" 2>/dev/null || true)")"
if [[ "$(jq -r '.ok // false' <<<"${set_state}")" != "true" ]]; then
    artifacts_json="$(jq -nc --arg reason "bio_update_failed" --argjson state "${set_state}" '{reason: $reason, state: $state}')"
    emit_json "failed" "Unable to update the X bio field." "${artifacts_json}"
    exit 0
fi

settings_bio_eval="$(python3 - "${bio_selector}" <<'PY'
import json
import sys

selector = sys.argv[1]
print(f"""(() => {{
  const node = document.querySelector({json.dumps(selector)});
  const value = node ? ("value" in node ? node.value : node.textContent || "") : "";
  return {{
    selector: {json.dumps(selector)},
    value,
    value_length: value.length,
    current_url: location.href,
    title: document.title
  }};
}})()""")
PY
)"
settings_bio_state="$(json_object_or_empty "$(browser_evaluate "${settings_bio_eval}" 2>/dev/null || true)")"

save_eval='(() => {
  const visible = (node) => {
    if (!node) return false;
    const rect = node.getBoundingClientRect();
    const style = window.getComputedStyle(node);
    return !!(rect.width && rect.height) && style.visibility !== "hidden" && style.display !== "none";
  };
  const buttons = [
    ...document.querySelectorAll("[data-testid=\"settingsDetailSave\"]"),
    ...document.querySelectorAll("[data-testid=\"Profile_Save_Button\"]"),
    ...document.querySelectorAll("button")
  ].filter(visible);
  const button = buttons.find((node) => /save/i.test(node.innerText || node.getAttribute("aria-label") || "")) || buttons[0];
  if (!button) return {clicked: false, reason: "save_button_missing", current_url: location.href, title: document.title};
  if (button.disabled || button.getAttribute("aria-disabled") === "true") return {clicked: false, reason: "save_button_disabled", current_url: location.href, title: document.title};
  button.click();
  return {clicked: true, current_url: location.href, title: document.title};
})()'

save_state="$(json_object_or_empty "$(browser_evaluate "${save_eval}" 2>/dev/null || true)")"
if [[ "$(jq -r '.clicked // false' <<<"${save_state}")" != "true" ]]; then
    artifacts_json="$(jq -nc --arg reason "save_click_failed" --argjson state "${save_state}" '{reason: $reason, state: $state}')"
    emit_json "failed" "Unable to save the X bio change." "${artifacts_json}"
    exit 0
fi

sleep 2
post_save_state="$(json_object_or_empty "$(browser_evaluate '(() => ({current_url: location.href, title: document.title}))()' 2>/dev/null || true)")"
post_save_url="$(jq -r '.current_url // empty' <<<"${post_save_state}")"

if [[ -z "${profile_url}" ]]; then
    find_profile_eval='(() => {
      const absolute = (href) => {
        try { return new URL(href, location.href).href; } catch (_) { return ""; }
      };
      const candidates = Array.from(document.querySelectorAll("a[href]"));
      const profileLink = candidates.find((node) => node.getAttribute("data-testid") === "AppTabBar_Profile_Link");
      if (profileLink) return {profile_url: absolute(profileLink.getAttribute("href")), source: "app_tab_profile_link", current_url: location.href, title: document.title};
      const allowed = /^\/[A-Za-z0-9_]{1,15}$/;
      const fallback = candidates.find((node) => allowed.test(node.getAttribute("href") || ""));
      if (fallback) return {profile_url: absolute(fallback.getAttribute("href")), source: "single_segment_profile_href", current_url: location.href, title: document.title};
      return {profile_url: "", source: "not_found", current_url: location.href, title: document.title};
    })()'
    profile_state="$(json_object_or_empty "$(browser_evaluate "${find_profile_eval}" 2>/dev/null || true)")"
    profile_url="$(jq -r '.profile_url // empty' <<<"${profile_state}")"
else
    profile_state="$(jq -nc --arg profile_url "${profile_url}" --arg current_url "${post_save_url}" '{profile_url: $profile_url, source: "request", current_url: $current_url}')"
fi

if [[ -z "${profile_url}" ]]; then
    artifacts_json="$(jq -nc \
        --arg reason "profile_url_missing" \
        --arg target_url "${target_url}" \
        --arg post_save_url "${post_save_url}" \
        --arg profile_url "" \
        --arg bio "${bio}" \
        --arg label "${label}" \
        --arg task_id "${task_id}" \
        --arg run_id "${run_id}" \
        --arg approval_id "${approval_id}" \
        --argjson save_state "${save_state}" \
        --argjson post_save_state "${post_save_state}" \
        --argjson profile_state "${profile_state}" \
        '{reason: $reason, target_url: $target_url, post_save_url: $post_save_url, profile_url: $profile_url, bio: $bio, label: $label, task_id: $task_id, run_id: $run_id, approval_id: $approval_id, save_clicked: true, save_state: $save_state, post_save_state: $post_save_state, profile_state: $profile_state, bio_verified: false}')"
    emit_json "failed" "Unable to resolve the X profile page for bio verification." "${artifacts_json}"
    exit 0
fi

if ! browser_navigate "${profile_url}" >/dev/null 2>&1; then
    artifacts_json="$(jq -nc \
        --arg reason "profile_navigation_failed" \
        --arg target_url "${target_url}" \
        --arg post_save_url "${post_save_url}" \
        --arg profile_url "${profile_url}" \
        --arg bio "${bio}" \
        --arg label "${label}" \
        --arg task_id "${task_id}" \
        --arg run_id "${run_id}" \
        --arg approval_id "${approval_id}" \
        --argjson save_state "${save_state}" \
        --argjson post_save_state "${post_save_state}" \
        --argjson profile_state "${profile_state}" \
        '{reason: $reason, target_url: $target_url, post_save_url: $post_save_url, profile_url: $profile_url, bio: $bio, label: $label, task_id: $task_id, run_id: $run_id, approval_id: $approval_id, save_clicked: true, save_state: $save_state, post_save_state: $post_save_state, profile_state: $profile_state, bio_verified: false}')"
    emit_json "failed" "Unable to navigate to the X profile page for bio verification." "${artifacts_json}"
    exit 0
fi

verify_eval="$(python3 - "${bio}" <<'PY'
import json
import sys
bio = sys.argv[1]
print(f"""(() => {{
  const bodyText = (document.body && document.body.innerText || "");
  const compact = (value) => (value || "").replace(/\\s+/g, " ").trim();
  const candidates = [];
  const push = (source, node) => {{
    if (!node) return;
    const text = compact(node.innerText || node.textContent || "");
    if (text && !candidates.some((item) => item.text === text)) {{
      candidates.push({{source, text}});
    }}
  }};
  for (const node of document.querySelectorAll("[data-testid='UserDescription'], [data-testid='UserProfileHeader_Items']")) {{
    push(node.getAttribute("data-testid") || "profile_candidate", node);
  }}
  for (const node of document.querySelectorAll("main [dir='auto'], main span")) {{
    const text = compact(node.innerText || node.textContent || "");
    if (text && (text.includes({json.dumps(bio)}) || {json.dumps(bio)}.includes(text))) {{
      push("matching_main_text", node);
    }}
    if (candidates.length >= 12) break;
  }}
  return {{
    current_url: location.href,
    title: document.title,
    bio_present: bodyText.includes({json.dumps(bio)}),
    body_text_sample: bodyText.slice(0, 4000),
    profile_bio_candidates: candidates.slice(0, 12)
  }};
}})()""")
PY
)"
verify_state='{}'
for _ in $(seq 1 20); do
    verify_state="$(json_object_or_empty "$(browser_evaluate "${verify_eval}" 2>/dev/null || true)")"
    if [[ "$(jq -r '.bio_present // false' <<<"${verify_state}")" == "true" ]]; then
        break
    fi
    sleep 0.5
done
current_url="$(jq -r '.current_url // empty' <<<"${verify_state}")"
title="$(jq -r '.title // empty' <<<"${verify_state}")"
bio_verified="$(jq -r '.bio_present // false' <<<"${verify_state}")"
updated_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
artifacts_json="$(jq -nc \
    --arg target_url "${target_url}" \
    --arg current_url "${current_url}" \
    --arg post_save_url "${post_save_url}" \
    --arg profile_url "${profile_url}" \
    --arg title "${title}" \
    --arg bio "${bio}" \
    --arg label "${label}" \
    --arg task_id "${task_id}" \
    --arg run_id "${run_id}" \
    --arg approval_id "${approval_id}" \
    --arg updated_at "${updated_at}" \
    --argjson save_state "${save_state}" \
    --argjson settings_bio_state "${settings_bio_state}" \
    --argjson post_save_state "${post_save_state}" \
    --argjson profile_state "${profile_state}" \
    --argjson verify_state "${verify_state}" \
    '{target_url: $target_url, current_url: $current_url, post_save_url: $post_save_url, profile_url: $profile_url, title: $title, observed_title: $title, bio: $bio, label: $label, task_id: $task_id, run_id: $run_id, approval_id: $approval_id, updated_at: $updated_at, save_clicked: true, bio_verified: ($verify_state.bio_present // false), settings_bio_state: $settings_bio_state, save_state: $save_state, post_save_state: $post_save_state, profile_state: $profile_state, verify_state: $verify_state, profile_body_text_sample: ($verify_state.body_text_sample // ""), profile_bio_candidates: ($verify_state.profile_bio_candidates // [])}')"

if [[ "${bio_verified}" != "true" ]]; then
    emit_json "failed" "X profile bio verification failed after save." "${artifacts_json}"
    exit 0
fi

emit_json "completed" "Applied approved X profile bio change and verified it on the X profile page." "${artifacts_json}"
