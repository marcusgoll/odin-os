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

canonicalize_x_status_url() {
    python3 - "$1" <<'PY'
from urllib.parse import urlparse
import sys

raw = (sys.argv[1] or "").strip()
if not raw:
    raise SystemExit(1)

parsed = urlparse(raw)
if parsed.scheme not in {"http", "https"}:
    raise SystemExit(1)
host = (parsed.hostname or "").strip().lower()
if host not in {"x.com", "www.x.com", "twitter.com", "www.twitter.com"}:
    raise SystemExit(1)
parts = [part for part in parsed.path.split("/") if part]
if len(parts) != 3 or parts[1] != "status" or not parts[0] or not parts[2].isdigit():
    raise SystemExit(1)
print(f"https://x.com/{parts[0]}/status/{parts[2]}")
PY
}

is_canonical_x_status_url() {
    canonicalize_x_status_url "$1" >/dev/null 2>&1
}

emit_json() {
    local status="$1"
    local tool_key="$2"
    local summary="$3"
    local artifacts_json="${4:-}"
    if [[ -z "${artifacts_json}" ]]; then
        artifacts_json='{}'
    fi

    python3 - "$status" "$tool_key" "$summary" "$artifacts_json" <<'PY'
import json
import sys

status, tool_key, summary, artifacts_raw = sys.argv[1:5]
artifacts = json.loads(artifacts_raw)
print(json.dumps({
    "status": status,
    "tool_key": tool_key,
    "summary": summary,
    "artifacts": artifacts,
}))
PY
}

sleep_ms() {
    local duration_ms="${1:-0}"
    if [[ "${duration_ms}" =~ ^[0-9]+$ ]] && [[ "${duration_ms}" != "0" ]]; then
        python3 - "${duration_ms}" <<'PY'
import sys
import time
time.sleep(max(int(sys.argv[1]), 0) / 1000.0)
PY
    fi
}

request_json="$(cat)"

mapfile -t request_fields < <(REQUEST_JSON="${request_json}" python3 - <<'PY'
import json
import os
import re

try:
    request = json.loads(os.environ.get("REQUEST_JSON") or "{}")
except Exception as exc:
    print("error")
    print(f"invalid request json: {exc}")
    raise SystemExit(0)

tool_key = str(request.get("tool_key") or "browser_x_post_publish").strip() or "browser_x_post_publish"
payload = request.get("input") or {}
post_text = str(payload.get("post_text") or "").strip()
content_kind = str(payload.get("content_kind") or "post").strip().lower() or "post"
in_reply_to_url = str(payload.get("in_reply_to_url") or "").strip()
label = str(payload.get("label") or "x-post-publish").strip() or "x-post-publish"
screenshot_path = str(payload.get("screenshot_path") or "").strip()
wait_ms = str(payload.get("wait_ms") or "4000").strip() or "4000"
headless = str(payload.get("headless") or "false").strip().lower()

if not post_text:
    print("error")
    print("post_text is required")
    raise SystemExit(0)
if content_kind not in {"post", "reply"}:
    print("error")
    print("content_kind must be post or reply")
    raise SystemExit(0)
if content_kind == "reply" and not in_reply_to_url:
    print("error")
    print("in_reply_to_url is required for reply publish")
    raise SystemExit(0)

safe_label = re.sub(r"[^a-z0-9._-]+", "-", label.lower()).strip("-") or "x-post-publish"

print("ok")
print(tool_key)
print(post_text)
print(content_kind)
print(in_reply_to_url)
print(label)
print(safe_label)
print(screenshot_path)
print(wait_ms)
print("true" if headless in {"1", "true", "yes"} else "false")
PY
)

if [[ "${request_fields[0]:-error}" != "ok" ]]; then
    emit_json "failed" "browser_x_post_publish" "${request_fields[1]:-invalid request}" '{"reason":"invalid_request"}'
    exit 0
fi

tool_key="${request_fields[1]}"
post_text="${request_fields[2]}"
content_kind="${request_fields[3]}"
in_reply_to_url="${request_fields[4]}"
label="${request_fields[5]}"
safe_label="${request_fields[6]}"
screenshot_path="${request_fields[7]}"
wait_ms="${request_fields[8]}"
headless="${request_fields[9]}"
browser_access_sh="$(resolve_browser_access_lib)"

if [[ ! -f "${browser_access_sh}" ]]; then
    artifacts_json="$(jq -nc --arg reason "browser_lib_missing" --arg browser_access_path "${browser_access_sh}" '{reason: $reason, browser_access_path: $browser_access_path}')"
    emit_json "failed" "${tool_key}" "Browser access library not found at ${browser_access_sh}." "${artifacts_json}"
    exit 0
fi

# shellcheck source=/dev/null
source "${browser_access_sh}"

cleanup() {
    browser_server_stop >/dev/null 2>&1 || true
}
trap cleanup EXIT

compose_url="https://x.com/compose/post"
target_url="${compose_url}"
if [[ "${content_kind}" == "reply" ]]; then
    target_url="${in_reply_to_url}"
fi
if declare -F browser_trusted_session_start >/dev/null 2>&1; then
    browser_trusted_session_start --url "${target_url}" >/dev/null 2>&1 || true
fi
if ! browser_server_health >/dev/null 2>&1; then
    if [[ "${headless}" == "true" ]]; then
        browser_server_start --url "${target_url}" --headless >/dev/null 2>&1 || true
    else
        browser_server_start --url "${target_url}" --headed >/dev/null 2>&1 || true
    fi
fi

if ! browser_server_health >/dev/null 2>&1; then
    emit_json "failed" "${tool_key}" "Unable to start browser X publish session." '{"reason":"browser_start_failed"}'
    exit 0
fi

compose_eval='(() => {
  const isVisible = (node) => {
    if (!node) return false;
    const rect = node.getBoundingClientRect();
    const style = window.getComputedStyle(node);
    return !!(rect.width && rect.height) && style.visibility !== "hidden" && style.display !== "none";
  };
  const composerCandidates = Array.from(document.querySelectorAll("[data-testid=\"tweetTextarea_0\"], [data-testid=\"tweetTextarea_1\"], div[role=\"textbox\"]"))
    .filter(isVisible)
    .sort((left, right) => {
      const leftRect = left.getBoundingClientRect();
      const rightRect = right.getBoundingClientRect();
      return (rightRect.width * rightRect.height) - (leftRect.width * leftRect.height);
    });
  const buttonCandidates = [
    ...Array.from(document.querySelectorAll("[data-testid=\"tweetButton\"]")),
    ...Array.from(document.querySelectorAll("[data-testid=\"tweetButtonInline\"]"))
  ].filter(isVisible);
  const composer = composerCandidates[0] || null;
  const button = buttonCandidates.find((candidate) => !candidate.disabled && candidate.getAttribute("aria-disabled") !== "true") ||
    buttonCandidates[0] ||
    null;
  return {
    composer_selector: composer ? (composer.getAttribute("data-testid") ? `[data-testid="${composer.getAttribute("data-testid")}"]` : "div[role=\"textbox\"]") : "",
    button_selector: button ? (button.getAttribute("data-testid") ? `[data-testid="${button.getAttribute("data-testid")}"]` : "") : "",
    current_url: location.href,
    title: document.title
  };
})()'

compose_state='{}'
composer_selector=""
button_selector=""
for _ in $(seq 1 20); do
    compose_state="$(browser_evaluate "${compose_eval}" 2>/dev/null || printf '{}')"
    composer_selector="$(jq -r '.composer_selector // empty' <<<"${compose_state}")"
    button_selector="$(jq -r '.button_selector // empty' <<<"${compose_state}")"
    if [[ -n "${composer_selector}" && -n "${button_selector}" ]]; then
        if [[ "${button_selector}" != '[data-testid="tweetButtonInline"]' || "${content_kind}" == "reply" ]]; then
            break
        fi
    fi
    python3 - <<'PY'
import time
time.sleep(0.2)
PY
done

if [[ -z "${composer_selector}" || -z "${button_selector}" ]]; then
    current_url="$(jq -r '.current_url // empty' <<<"${compose_state}" 2>/dev/null || true)"
    title_for_error="$(jq -r '.title // empty' <<<"${compose_state}" 2>/dev/null || true)"
    artifacts_json="$(jq -nc --arg reason "compose_surface_missing" --arg current_url "${current_url}" --arg title "${title_for_error}" '{reason: $reason, current_url: $current_url, title: $title}')"
    emit_json "failed" "${tool_key}" "Unable to find the X publish surface." "${artifacts_json}"
    exit 0
fi
if [[ "${button_selector}" == '[data-testid="tweetButtonInline"]' && "${content_kind}" != "reply" ]]; then
    current_url="$(jq -r '.current_url // empty' <<<"${compose_state}" 2>/dev/null || true)"
    title_for_error="$(jq -r '.title // empty' <<<"${compose_state}" 2>/dev/null || true)"
    artifacts_json="$(jq -nc --arg reason "compose_surface_unsettled" --arg current_url "${current_url}" --arg title "${title_for_error}" '{reason: $reason, current_url: $current_url, title: $title}')"
    emit_json "failed" "${tool_key}" "X publish surface did not settle onto the main publish button." "${artifacts_json}"
    exit 0
fi

if ! browser_type_selector "${composer_selector}" "${post_text}" >/dev/null 2>&1; then
    emit_json "failed" "${tool_key}" "Unable to type into the X composer." '{"reason":"compose_type_failed"}'
    exit 0
fi

button_ready_eval="$(python3 - "${button_selector}" <<'PY'
import json
import sys

selector = sys.argv[1]
expression = f"""(() => {{
  const button = document.querySelector({json.dumps(selector)});
  if (!button) {{
    return {{
      button_ready: false,
      reason: "button_missing",
      current_url: location.href,
      title: document.title
    }};
  }}
  const rect = button.getBoundingClientRect();
  const style = window.getComputedStyle(button);
  const visible = !!(rect.width && rect.height) && style.visibility !== "hidden" && style.display !== "none";
  const enabled = !button.disabled && button.getAttribute("aria-disabled") !== "true";
  return {{
    button_ready: visible && enabled,
    current_url: location.href,
    title: document.title
  }};
}})()"""
print(expression)
PY
)"

button_state='{}'
button_ready="false"
for _ in $(seq 1 20); do
    button_state="$(browser_evaluate "${button_ready_eval}" 2>/dev/null || printf '{}')"
    if [[ "$(jq -r '.button_ready // false' <<<"${button_state}")" == "true" ]]; then
        button_ready="true"
        break
    fi
    python3 - <<'PY'
import time
time.sleep(0.2)
PY
done

if [[ "${button_ready}" != "true" ]]; then
    current_url="$(jq -r '.current_url // empty' <<<"${button_state}" 2>/dev/null || true)"
    title_for_error="$(jq -r '.title // empty' <<<"${button_state}" 2>/dev/null || true)"
    artifacts_json="$(jq -nc --arg reason "post_button_not_ready" --arg current_url "${current_url}" --arg title "${title_for_error}" '{reason: $reason, current_url: $current_url, title: $title}')"
    emit_json "failed" "${tool_key}" "The selected X post button did not become ready." "${artifacts_json}"
    exit 0
fi

button_click_eval="$(python3 - "${button_selector}" <<'PY'
import json
import sys

selector = sys.argv[1]
expression = f"""(() => {{
  const button = document.querySelector({json.dumps(selector)});
  if (!button) {{
    return {{
      clicked: false,
      reason: "button_missing",
      current_url: location.href,
      title: document.title
    }};
  }}
  if (typeof button.focus === "function") {{
    try {{ button.focus(); }} catch {{}}
  }}
  if (typeof button.click === "function") {{
    button.click();
    return {{
      clicked: true,
      current_url: location.href,
      title: document.title
    }};
  }}
  return {{
    clicked: false,
    reason: "button_click_unavailable",
    current_url: location.href,
    title: document.title
  }};
}})()"""
print(expression)
PY
)"

button_click_state="$(browser_evaluate "${button_click_eval}" 2>/dev/null || printf '{}')"
if [[ "$(jq -r '.clicked // false' <<<"${button_click_state}")" != "true" ]]; then
    if ! browser_click_selector "${button_selector}" >/dev/null 2>&1; then
        emit_json "failed" "${tool_key}" "Unable to click the X post button." '{"reason":"post_click_failed"}'
        exit 0
    fi
fi

if [[ "${wait_ms}" =~ ^[0-9]+$ ]] && [[ "${wait_ms}" != "0" ]]; then
    python3 - "${wait_ms}" <<'PY'
import sys
import time
time.sleep(max(int(sys.argv[1]), 0) / 1000.0)
PY
fi

publish_eval="$(python3 - "${post_text}" <<'PY'
import json
import sys

snippet = sys.argv[1][:80]
expression = f"""(() => {{
  const snippet = {json.dumps(snippet)};
  const current = location.href;
  const anchors = Array.from(document.querySelectorAll('a[href*="/status/"]')).map((node) => node.href).filter(Boolean);
  let publishUrl = /\\/status\\/\\d+/.test(current) ? current : "";
  if (!publishUrl && snippet) {{
    const article = Array.from(document.querySelectorAll('article')).find((node) => (node.innerText || '').includes(snippet));
    if (article) {{
      const match = article.querySelector('a[href*="/status/"]');
      if (match && match.href) publishUrl = match.href;
    }}
  }}
  if (!publishUrl) {{
    publishUrl = anchors.find((href) => /\\/status\\/\\d+/.test(href)) || "";
  }}
  return {{
    publish_url: publishUrl,
    final_url: location.href,
    title: document.title
  }};
}})()"""
print(expression)
PY
)"

canonical_reply_target=""
if [[ "${content_kind}" == "reply" ]]; then
    canonical_reply_target="$(canonicalize_x_status_url "${in_reply_to_url}" 2>/dev/null || true)"
fi

read_publish_state() {
    publish_state="$(browser_evaluate "${publish_eval}" 2>/dev/null || printf '{}')"
    publish_url="$(jq -r '.publish_url // empty' <<<"${publish_state}")"
    final_url="$(jq -r '.final_url // empty' <<<"${publish_state}")"
    title="$(jq -r '.title // empty' <<<"${publish_state}")"
    canonical_publish_url="$(canonicalize_x_status_url "${publish_url}" 2>/dev/null || true)"
}

wait_for_publish_url() {
    local attempts="${1:-20}"
    local sleep_between_ms="${2:-500}"
    local attempt=""

    for attempt in $(seq 1 "${attempts}"); do
        read_publish_state
        if [[ -n "${canonical_publish_url}" ]]; then
            if [[ "${content_kind}" != "reply" || -z "${canonical_reply_target}" || "${canonical_publish_url}" != "${canonical_reply_target}" ]]; then
                return 0
            fi
        fi
        if [[ "${attempt}" -lt "${attempts}" ]]; then
            sleep_ms "${sleep_between_ms}"
        fi
    done

    return 1
}

read_publish_state

if [[ "${content_kind}" == "reply" && "${final_url}" == *"/compose/post"* && ( -z "${canonical_publish_url}" || ( -n "${canonical_reply_target}" && "${canonical_publish_url}" == "${canonical_reply_target}" ) ) ]]; then
    modal_button_selector='[data-testid="tweetButton"]'
    modal_button_ready_eval="$(python3 - "${modal_button_selector}" <<'PY'
import json
import sys

selector = sys.argv[1]
expression = f"""(() => {{
  const button = document.querySelector({json.dumps(selector)});
  if (!button) {{
    return {{
      button_ready: false,
      reason: "button_missing",
      current_url: location.href,
      title: document.title
    }};
  }}
  const rect = button.getBoundingClientRect();
  const style = window.getComputedStyle(button);
  const visible = !!(rect.width && rect.height) && style.visibility !== "hidden" && style.display !== "none";
  const enabled = !button.disabled && button.getAttribute("aria-disabled") !== "true";
  return {{
    button_ready: visible && enabled,
    current_url: location.href,
    title: document.title
  }};
}})()"""
print(expression)
PY
)"

    modal_button_state='{}'
    modal_button_ready="false"
    for _ in $(seq 1 20); do
        modal_button_state="$(browser_evaluate "${modal_button_ready_eval}" 2>/dev/null || printf '{}')"
        if [[ "$(jq -r '.button_ready // false' <<<"${modal_button_state}")" == "true" ]]; then
            modal_button_ready="true"
            break
        fi
        python3 - <<'PY'
import time
time.sleep(0.2)
PY
    done

    if [[ "${modal_button_ready}" != "true" ]]; then
        current_url="$(jq -r '.current_url // empty' <<<"${modal_button_state}" 2>/dev/null || true)"
        title_for_error="$(jq -r '.title // empty' <<<"${modal_button_state}" 2>/dev/null || true)"
        artifacts_json="$(jq -nc --arg reason "modal_post_button_not_ready" --arg current_url "${current_url}" --arg title "${title_for_error}" '{reason: $reason, current_url: $current_url, title: $title}')"
        emit_json "failed" "${tool_key}" "The final X post button did not become ready." "${artifacts_json}"
        exit 0
    fi

    modal_button_click_eval="$(python3 - "${modal_button_selector}" <<'PY'
import json
import sys

selector = sys.argv[1]
expression = f"""(() => {{
  const button = document.querySelector({json.dumps(selector)});
  if (!button) {{
    return {{
      clicked: false,
      reason: "button_missing",
      current_url: location.href,
      title: document.title
    }};
  }}
  if (typeof button.focus === "function") {{
    try {{ button.focus(); }} catch {{}}
  }}
  if (typeof button.click === "function") {{
    button.click();
    return {{
      clicked: true,
      current_url: location.href,
      title: document.title
    }};
  }}
  return {{
    clicked: false,
    reason: "button_click_unavailable",
    current_url: location.href,
    title: document.title
  }};
}})()"""
print(expression)
PY
)"

    modal_button_click_state="$(browser_evaluate "${modal_button_click_eval}" 2>/dev/null || printf '{}')"
    if [[ "$(jq -r '.clicked // false' <<<"${modal_button_click_state}")" != "true" ]]; then
        if ! browser_click_selector "${modal_button_selector}" >/dev/null 2>&1; then
            emit_json "failed" "${tool_key}" "Unable to click the final X post button." '{"reason":"final_post_click_failed"}'
            exit 0
        fi
    fi

    sleep_ms "${wait_ms}"
    read_publish_state
fi

wait_for_publish_url 20 500 || true

if [[ -z "${canonical_publish_url}" ]]; then
    artifacts_json="$(jq -nc --arg reason "publish_url_missing" --arg final_url "${final_url}" --arg title "${title}" '{reason: $reason, final_url: $final_url, title: $title}')"
    emit_json "failed" "${tool_key}" "Unable to verify the resulting X post URL after publish." "${artifacts_json}"
    exit 0
fi
if [[ "${content_kind}" == "reply" && -n "${canonical_reply_target}" && "${canonical_publish_url}" == "${canonical_reply_target}" ]]; then
    artifacts_json="$(jq -nc --arg reason "publish_url_matches_reply_target" --arg publish_url "${publish_url}" --arg final_url "${final_url}" --arg title "${title}" '{reason: $reason, publish_url: $publish_url, final_url: $final_url, title: $title}')"
    emit_json "failed" "${tool_key}" "Unable to verify the resulting X post URL after publish." "${artifacts_json}"
    exit 0
fi
publish_url="${canonical_publish_url}"

publish_root="${ODIN_DIR:-${ODIN_ROOT:-${HOME}/.odin}}/browser-state/social-publish"
mkdir -p "${publish_root}"
if [[ -z "${screenshot_path}" ]]; then
    screenshot_path="${publish_root}/${safe_label}.png"
fi

browser_bc_screenshot --output "${screenshot_path}" >/dev/null 2>&1 || true
published_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
summary="Published approved X post through Browser Control."
if [[ "${content_kind}" == "reply" ]]; then
    summary="Published approved X reply through Browser Control."
fi
artifacts_json="$(jq -nc \
    --arg publish_url "${publish_url}" \
    --arg final_url "${final_url}" \
    --arg title "${title}" \
    --arg content_kind "${content_kind}" \
    --arg in_reply_to_url "${in_reply_to_url}" \
    --arg label "${label}" \
    --arg screenshot_path "${screenshot_path}" \
    --arg published_at "${published_at}" \
    --arg posted_text "${post_text}" \
    '{publish_url: $publish_url, final_url: $final_url, title: $title, content_kind: $content_kind, in_reply_to_url: $in_reply_to_url, label: $label, screenshot_path: $screenshot_path, published_at: $published_at, posted_text: $posted_text}')"
emit_json "completed" "${tool_key}" "${summary}" "${artifacts_json}"
