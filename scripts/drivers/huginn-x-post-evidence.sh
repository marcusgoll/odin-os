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
    local status="$1"
    local tool_key="$2"
    local summary="$3"
    local artifacts_json="${4:-{}}"

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

request_json="$(cat)"

mapfile -t request_fields < <(REQUEST_JSON="${request_json}" python3 - <<'PY'
import json
import os
import re
from urllib.parse import urlparse

ALLOWED_HOSTS = {"x.com", "www.x.com", "twitter.com", "www.twitter.com"}

try:
    request = json.loads(os.environ.get("REQUEST_JSON") or "{}")
except Exception as exc:
    print("error")
    print(f"invalid request json: {exc}")
    raise SystemExit(0)

tool_key = str(request.get("tool_key") or "browser_x_post_visible_evidence").strip() or "browser_x_post_visible_evidence"
payload = request.get("input") or {}
target_url = str(payload.get("target_url") or "").strip()
label = str(payload.get("label") or "x-post-evidence").strip() or "x-post-evidence"
screenshot_path = str(payload.get("screenshot_path") or "").strip()
wait_ms = str(payload.get("wait_ms") or "2000").strip() or "2000"
headless = str(payload.get("headless") or "false").strip().lower()

if not target_url:
    print("error")
    print("target_url is required")
    raise SystemExit(0)

parsed = urlparse(target_url)
host = (parsed.hostname or "").strip().lower()
if host not in ALLOWED_HOSTS:
    print("error")
    print("target_url must use an allowed X host")
    raise SystemExit(0)

safe_label = re.sub(r"[^a-z0-9._-]+", "-", label.lower()).strip("-") or "x-post-evidence"

print("ok")
print(tool_key)
print(target_url)
print(label)
print(safe_label)
print(screenshot_path)
print(wait_ms)
print("false" if headless in {"0", "false", "no"} else "true")
PY
)

if [[ "${request_fields[0]:-error}" != "ok" ]]; then
    emit_json "failed" "browser_x_post_visible_evidence" "${request_fields[1]:-invalid request}" '{"reason":"invalid_request"}'
    exit 0
fi

tool_key="${request_fields[1]}"
target_url="${request_fields[2]}"
label="${request_fields[3]}"
safe_label="${request_fields[4]}"
screenshot_path="${request_fields[5]}"
wait_ms="${request_fields[6]}"
headless="${request_fields[7]}"
browser_access_sh="$(resolve_browser_access_lib)"

if [[ ! -f "${browser_access_sh}" ]]; then
    emit_json "failed" "${tool_key}" "Browser access library not found at ${browser_access_sh}." "$(python3 - "${target_url}" "${label}" "${browser_access_sh}" <<'PY'
import json
import sys

target_url, label, browser_access_sh = sys.argv[1:4]
print(json.dumps({
    "target_url": target_url,
    "label": label,
    "reason": "browser_lib_missing",
    "browser_access_path": browser_access_sh,
}))
PY
)"
    exit 0
fi

# shellcheck source=/dev/null
source "${browser_access_sh}"

cleanup() {
    browser_server_stop >/dev/null 2>&1 || true
}
trap cleanup EXIT

evidence_root="${ODIN_DIR:-${ODIN_ROOT:-${HOME}/.odin}}/browser-state/social-evidence"
mkdir -p "${evidence_root}"

if [[ -z "${screenshot_path}" ]]; then
    screenshot_path="${evidence_root}/${safe_label}.png"
fi
snapshot_path="${evidence_root}/${safe_label}.txt"

launch_mode="--headless"
if [[ "${headless}" == "false" ]]; then
    launch_mode="--headed"
fi

if [[ "${headless}" == "false" ]] && declare -F browser_trusted_session_start >/dev/null 2>&1; then
    browser_trusted_session_start --url "${target_url}" >/dev/null 2>&1 || true
else
    browser_server_start --url "${target_url}" "${launch_mode}" >/dev/null 2>&1 || true
fi

if ! browser_server_health >/dev/null 2>&1; then
    emit_json "failed" "${tool_key}" "Unable to start browser X evidence session." "$(python3 - "${target_url}" "${label}" "${launch_mode}" <<'PY'
import json
import sys

target_url, label, launch_mode = sys.argv[1:4]
print(json.dumps({
    "target_url": target_url,
    "label": label,
    "launch_mode": launch_mode,
    "reason": "browser_start_failed",
}))
PY
)"
    exit 0
fi

if [[ "${wait_ms}" =~ ^[0-9]+$ ]] && [[ "${wait_ms}" != "0" ]]; then
    python3 - "${wait_ms}" <<'PY'
import sys
import time

time.sleep(max(int(sys.argv[1]), 0) / 1000.0)
PY
fi

evaluate_js="$(cat <<'EOF'
(() => {
  const clean = (value) => String(value || '').replace(/\s+/g, ' ').trim();
  const article = document.querySelector('article');
  const articleText = clean(article ? article.innerText : '');
  const postText = clean(
    Array.from(document.querySelectorAll('[data-testid="tweetText"]'))
      .map((node) => node.textContent || '')
      .join(' ')
  );
  const userNameRoot = article ? article.querySelector('[data-testid="User-Name"]') : null;
  const spans = userNameRoot ? Array.from(userNameRoot.querySelectorAll('span')) : [];
  const authorDisplayName = clean(spans.length > 0 ? spans[0].textContent || '' : '');
  const handleMatch = articleText.match(/@[A-Za-z0-9_]{1,15}/);
  const handle = handleMatch ? handleMatch[0] : '';
  const timeEl = article ? article.querySelector('time') : null;
  const metricText = (testId) => {
    const root = article ? article.querySelector(`[data-testid="${testId}"]`) : null;
    if (!root) return '';
    const nested = root.querySelector('[dir="ltr"], span[data-testid="app-text-transition-container"], span');
    return clean(nested ? nested.textContent || '' : root.textContent || '');
  };
  const analyticsRoot = article
    ? article.querySelector('[data-testid="analytics"]') || article.querySelector('a[href*="/analytics"]')
    : null;
  const viewCount = clean(analyticsRoot ? analyticsRoot.textContent || '' : '');
  return {
    final_url: location.href,
    title: document.title,
    post_text: postText,
    author_display_name: authorDisplayName,
    author_handle: handle,
    timestamp: timeEl ? clean(timeEl.getAttribute('datetime') || '') : '',
    reply_count: metricText('reply'),
    repost_count: metricText('retweet'),
    like_count: metricText('like'),
    bookmark_count: metricText('bookmark'),
    view_count: viewCount,
  };
})()
EOF
)"

set +e
health_json="$(browser_server_health 2>/dev/null)"
health_status=$?
snapshot_text="$(browser_snapshot 2>/dev/null)"
snapshot_status=$?
screenshot_result="$(browser_bc_screenshot --output "${screenshot_path}" 2>/dev/null)"
screenshot_status=$?
evaluate_result="$(browser_evaluate "${evaluate_js}" 2>/dev/null)"
evaluate_status=$?
set -e

if [[ ${snapshot_status} -eq 0 ]]; then
    printf '%s\n' "${snapshot_text}" >"${snapshot_path}"
fi

if [[ ${health_status} -ne 0 || -z "${health_json}" ]]; then
    emit_json "failed" "${tool_key}" "Browser X evidence health check failed." "$(python3 - "${target_url}" "${label}" "${screenshot_path}" "${snapshot_path}" <<'PY'
import json
import sys

target_url, label, screenshot_path, snapshot_path = sys.argv[1:5]
print(json.dumps({
    "target_url": target_url,
    "label": label,
    "screenshot_path": screenshot_path,
    "snapshot_path": snapshot_path,
    "reason": "healthcheck_failed",
}))
PY
)"
    exit 0
fi

ODIN_X_EVIDENCE_HEALTH_JSON="${health_json}" \
ODIN_X_EVIDENCE_SNAPSHOT_TEXT="${snapshot_text}" \
ODIN_X_EVIDENCE_EVALUATE_RESULT="${evaluate_result}" \
ODIN_X_EVIDENCE_EVALUATE_STATUS="${evaluate_status}" \
python3 - "${tool_key}" "${target_url}" "${label}" "${screenshot_result}" "${snapshot_path}" "${wait_ms}" "${launch_mode}" "${screenshot_status}" <<'PY'
import json
import os
import sys

tool_key, target_url, label, screenshot_path, snapshot_path, wait_ms, launch_mode, screenshot_status = sys.argv[1:9]
snapshot = os.environ.get("ODIN_X_EVIDENCE_SNAPSHOT_TEXT", "").strip()
evaluate_status = int(os.environ.get("ODIN_X_EVIDENCE_EVALUATE_STATUS", "1"))
evaluate_raw = os.environ.get("ODIN_X_EVIDENCE_EVALUATE_RESULT", "").strip()

try:
    health = json.loads(os.environ.get("ODIN_X_EVIDENCE_HEALTH_JSON", "{}"))
except Exception as exc:
    print(json.dumps({
        "status": "failed",
        "tool_key": tool_key,
        "summary": f"Unable to decode browser X evidence health response: {exc}",
        "artifacts": {
            "target_url": target_url,
            "label": label,
            "reason": "invalid_health_response",
        },
    }))
    raise SystemExit(0)

evaluated = {}
if evaluate_status == 0 and evaluate_raw:
    try:
        evaluated = json.loads(evaluate_raw)
    except Exception:
        evaluated = {}

excerpt = " ".join(snapshot.split())
if len(excerpt) > 320:
    excerpt = excerpt[:317] + "..."

browser_ok = bool(health.get("browser"))
page_ok = bool(health.get("page"))
# Trusted attached sessions can report browser=false while a live page is already attached.
browser_ready = browser_ok or page_ok
status = "completed" if browser_ready and page_ok and screenshot_status == "0" else "failed"
summary = f"Captured visible X post evidence for {label}."
if status != "completed":
    summary = f"Browser X evidence could not confirm a ready X post page for {label}."

artifacts = {
    "target_url": target_url,
    "final_url": str(evaluated.get("final_url") or health.get("url") or ""),
    "title": str(evaluated.get("title") or health.get("title") or ""),
    "label": label,
    "screenshot_path": screenshot_path,
    "snapshot_path": snapshot_path,
    "snapshot_excerpt": excerpt,
    "wait_ms": wait_ms,
    "launch_mode": launch_mode,
    "post_text": str(evaluated.get("post_text") or ""),
    "author_display_name": str(evaluated.get("author_display_name") or ""),
    "author_handle": str(evaluated.get("author_handle") or ""),
    "timestamp": str(evaluated.get("timestamp") or ""),
    "reply_count": str(evaluated.get("reply_count") or ""),
    "repost_count": str(evaluated.get("repost_count") or ""),
    "like_count": str(evaluated.get("like_count") or ""),
    "bookmark_count": str(evaluated.get("bookmark_count") or ""),
    "view_count": str(evaluated.get("view_count") or ""),
}

print(json.dumps({
    "status": status,
    "tool_key": tool_key,
    "summary": summary,
    "artifacts": artifacts,
}))
PY
