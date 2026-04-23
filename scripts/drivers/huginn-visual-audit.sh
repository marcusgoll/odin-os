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

try:
    request = json.loads(os.environ.get("REQUEST_JSON") or "{}")
except Exception as exc:
    print("error")
    print(f"invalid request json: {exc}")
    raise SystemExit(0)

tool_key = str(request.get("tool_key") or "browser_visual_audit").strip() or "browser_visual_audit"
payload = request.get("input") or {}
target_url = str(payload.get("target_url") or "").strip()
label = str(payload.get("label") or "visual-audit").strip() or "visual-audit"
screenshot_path = str(payload.get("screenshot_path") or "").strip()
wait_ms = str(payload.get("wait_ms") or "2000").strip() or "2000"
allow_private_host = str(payload.get("allow_private_host") or "false").strip().lower()
headless = str(payload.get("headless") or "true").strip().lower()

if not target_url:
    print("error")
    print("target_url is required")
    raise SystemExit(0)

safe_label = re.sub(r"[^a-z0-9._-]+", "-", label.lower()).strip("-") or "visual-audit"

print("ok")
print(tool_key)
print(target_url)
print(label)
print(safe_label)
print(screenshot_path)
print(wait_ms)
print("true" if allow_private_host in {"1", "true", "yes"} else "false")
print("false" if headless in {"0", "false", "no"} else "true")
PY
)

if [[ "${request_fields[0]:-error}" != "ok" ]]; then
    emit_json "failed" "browser_visual_audit" "${request_fields[1]:-invalid request}" '{"reason":"invalid_request"}'
    exit 0
fi

tool_key="${request_fields[1]}"
target_url="${request_fields[2]}"
label="${request_fields[3]}"
safe_label="${request_fields[4]}"
screenshot_path="${request_fields[5]}"
wait_ms="${request_fields[6]}"
allow_private_host="${request_fields[7]}"
headless="${request_fields[8]}"
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

if [[ "${allow_private_host}" == "true" ]]; then
    export ODIN_BROWSER_DOMAIN_DENYLIST=""
fi

if [[ -z "${screenshot_path}" ]]; then
    screenshot_root="${ODIN_DIR:-${ODIN_ROOT:-${HOME}/.odin}}/browser-state/visual-audit"
    mkdir -p "${screenshot_root}"
    screenshot_path="${screenshot_root}/${safe_label}.png"
fi

launch_mode="--headless"
if [[ "${headless}" == "false" ]]; then
    launch_mode="--headed"
fi

if [[ "${headless}" == "false" ]] && declare -F browser_trusted_session_start >/dev/null 2>&1; then
    browser_trusted_session_start --url "${target_url}" >/dev/null 2>&1 || true
fi

if ! browser_server_health >/dev/null 2>&1; then
    if ! browser_server_start --url "${target_url}" "${launch_mode}" >/dev/null 2>&1; then
        emit_json "failed" "${tool_key}" "Unable to start browser visual audit session." "$(python3 - "${target_url}" "${label}" "${launch_mode}" <<'PY'
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
fi

if ! browser_server_health >/dev/null 2>&1; then
    emit_json "failed" "${tool_key}" "Unable to start browser visual audit session." "$(python3 - "${target_url}" "${label}" "${launch_mode}" <<'PY'
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

set +e
health_json="$(browser_server_health 2>/dev/null)"
health_status=$?
snapshot_text="$(browser_snapshot 2>/dev/null)"
screenshot_result="$(browser_bc_screenshot --output "${screenshot_path}" 2>/dev/null)"
set -e

if [[ ${health_status} -ne 0 || -z "${health_json}" ]]; then
    emit_json "failed" "${tool_key}" "Browser visual audit health check failed." "$(python3 - "${target_url}" "${label}" "${screenshot_path}" <<'PY'
import json
import sys

target_url, label, screenshot_path = sys.argv[1:4]
print(json.dumps({
    "target_url": target_url,
    "label": label,
    "screenshot_path": screenshot_path,
    "reason": "healthcheck_failed",
}))
PY
)"
    exit 0
fi

ODIN_VISUAL_HEALTH_JSON="${health_json}" ODIN_VISUAL_SNAPSHOT_TEXT="${snapshot_text}" python3 - "${tool_key}" "${target_url}" "${label}" "${screenshot_result}" "${wait_ms}" "${launch_mode}" <<'PY'
import json
import os
import sys

tool_key, target_url, label, screenshot_path, wait_ms, launch_mode = sys.argv[1:7]
snapshot = os.environ.get("ODIN_VISUAL_SNAPSHOT_TEXT", "").strip()
try:
    health = json.loads(os.environ.get("ODIN_VISUAL_HEALTH_JSON", "{}"))
except Exception as exc:
    print(json.dumps({
        "status": "failed",
        "tool_key": tool_key,
        "summary": f"Unable to decode browser visual audit health response: {exc}",
        "artifacts": {
            "target_url": target_url,
            "label": label,
            "reason": "invalid_health_response",
        },
    }))
    raise SystemExit(0)

excerpt = " ".join(snapshot.split())
if len(excerpt) > 320:
    excerpt = excerpt[:317] + "..."

final_url = str(health.get("url") or "")
title = str(health.get("title") or "")
browser_ok = bool(health.get("browser"))
page_ok = bool(health.get("page"))
# Trusted attached sessions can report browser=false while a live page is already attached.
browser_ready = browser_ok or page_ok

status = "completed" if browser_ready and page_ok else "failed"
summary = f"Captured browser visual audit evidence for {label}."
if status != "completed":
    summary = f"Browser visual audit could not confirm a ready page for {label}."

print(json.dumps({
    "status": status,
    "tool_key": tool_key,
    "summary": summary,
    "artifacts": {
        "target_url": target_url,
        "final_url": final_url,
        "title": title,
        "label": label,
        "screenshot_path": screenshot_path,
        "snapshot_excerpt": excerpt,
        "wait_ms": wait_ms,
        "launch_mode": launch_mode,
    },
}))
PY
