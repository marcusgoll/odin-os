#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_BROWSER_ACCESS_SH="${SCRIPT_DIR}/../browser/browser-access.sh"

emit_json() {
    local status="$1" summary="$2" artifacts_json="${3:-{}}"
    jq -nc \
        --arg status "${status}" \
        --arg summary "${summary}" \
        --argjson artifacts "${artifacts_json}" \
        '{status: $status, tool_key: "browser_x_profile_bio_update", summary: $summary, artifacts: $artifacts}'
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
label = str(payload.get("label") or "x-bio-update").strip() or "x-bio-update"
task_id = str(payload.get("task_id") or "").strip()
run_id = str(payload.get("run_id") or "").strip()
approval_id = str(payload.get("approval_id") or "").strip()

parsed = urlparse(target_url)
host = (parsed.hostname or "").strip().lower()
if not bio:
    print("error")
    print("bio is required")
elif len(bio) > 160:
    print("error")
    print("bio must be 160 characters or less")
elif parsed.scheme not in {"http", "https"} or host not in {"x.com", "www.x.com", "twitter.com", "www.twitter.com"}:
    print("error")
    print("target_url must be an X URL")
else:
    print("ok")
    print(bio)
    print(target_url)
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
label="${request_fields[3]}"
task_id="${request_fields[4]}"
run_id="${request_fields[5]}"
approval_id="${request_fields[6]}"

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

cleanup() {
    browser_server_stop >/dev/null 2>&1 || true
}
trap cleanup EXIT

if ! browser_trusted_session_start --url "${target_url}" >/dev/null 2>&1; then
    emit_json "failed" "Unable to start trusted X browser session." '{"reason":"browser_start_failed"}'
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
    bio_state="$(browser_evaluate "${find_bio_eval}" 2>/dev/null || printf '{}')"
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

set_state="$(browser_evaluate "${set_bio_eval}" 2>/dev/null || printf '{}')"
if [[ "$(jq -r '.ok // false' <<<"${set_state}")" != "true" ]]; then
    artifacts_json="$(jq -nc --arg reason "bio_update_failed" --argjson state "${set_state}" '{reason: $reason, state: $state}')"
    emit_json "failed" "Unable to update the X bio field." "${artifacts_json}"
    exit 0
fi

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

save_state="$(browser_evaluate "${save_eval}" 2>/dev/null || printf '{}')"
if [[ "$(jq -r '.clicked // false' <<<"${save_state}")" != "true" ]]; then
    artifacts_json="$(jq -nc --arg reason "save_click_failed" --argjson state "${save_state}" '{reason: $reason, state: $state}')"
    emit_json "failed" "Unable to save the X bio change." "${artifacts_json}"
    exit 0
fi

sleep 2
verify_eval="$(python3 - "${bio}" <<'PY'
import json
import sys
bio = sys.argv[1]
print(f"""(() => {{
  return {{
    current_url: location.href,
    title: document.title,
    bio_present: (document.body && document.body.innerText || "").includes({json.dumps(bio)})
  }};
}})()""")
PY
)"
verify_state="$(browser_evaluate "${verify_eval}" 2>/dev/null || printf '{}')"
current_url="$(jq -r '.current_url // empty' <<<"${verify_state}")"
title="$(jq -r '.title // empty' <<<"${verify_state}")"
updated_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
artifacts_json="$(jq -nc \
    --arg target_url "${target_url}" \
    --arg current_url "${current_url}" \
    --arg title "${title}" \
    --arg bio "${bio}" \
    --arg label "${label}" \
    --arg task_id "${task_id}" \
    --arg run_id "${run_id}" \
    --arg approval_id "${approval_id}" \
    --arg updated_at "${updated_at}" \
    --argjson verify_state "${verify_state}" \
    '{target_url: $target_url, current_url: $current_url, title: $title, bio: $bio, label: $label, task_id: $task_id, run_id: $run_id, approval_id: $approval_id, updated_at: $updated_at, verify_state: $verify_state}')"
emit_json "completed" "Applied approved X profile bio change through Browser Control." "${artifacts_json}"
