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
    local artifacts_json="${4:-}"
    if [[ -z "${artifacts_json}" ]]; then
        artifacts_json="{}"
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
}, separators=(",", ":")))
PY
}

blocked_json() {
    local reason="$1"
    local recipe_key="$2"
    local target_url="$3"
    local extra_json="${4:-}"
    if [[ -z "${extra_json}" ]]; then
        extra_json="{}"
    fi
    python3 - "$reason" "$recipe_key" "$target_url" "$extra_json" <<'PY'
import json
import sys

reason, recipe_key, target_url, extra_raw = sys.argv[1:5]
extra = json.loads(extra_raw)
payload = {
    "recipe_key": recipe_key,
    "target_url": target_url,
    "intervention_reason": reason,
}
payload.update(extra)
print(json.dumps(payload, separators=(",", ":")))
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

tool_key = str(request.get("tool_key") or "browser_dom_fast_lane").strip() or "browser_dom_fast_lane"
payload = request.get("input") or {}
recipe_key = str(payload.get("recipe_key") or "").strip()
target_url = str(payload.get("target_url") or payload.get("url") or "").strip()
label = str(payload.get("label") or recipe_key or "dom-fast-lane").strip() or "dom-fast-lane"
screenshot_path = str(payload.get("screenshot_path") or "").strip()
wait_ms = str(payload.get("wait_ms") or "0").strip() or "0"
headless = str(payload.get("headless") or "true").strip().lower()
allowed_domain = str(payload.get("allowed_domain") or "").strip().lower()
safe_label = re.sub(r"[^a-z0-9._-]+", "-", label.lower()).strip("-") or "dom-fast-lane"

if not target_url:
    print("error")
    print("target_url is required")
    raise SystemExit(0)
if not recipe_key:
    print("error")
    print("recipe_key is required")
    raise SystemExit(0)

print("ok")
print(tool_key)
print(recipe_key)
print(target_url)
print(label)
print(safe_label)
print(screenshot_path)
print(wait_ms)
print("false" if headless in {"0", "false", "no"} else "true")
print(allowed_domain)
PY
)

if [[ "${request_fields[0]:-error}" != "ok" ]]; then
    emit_json "failed" "browser_dom_fast_lane" "${request_fields[1]:-invalid request}" '{"reason":"invalid_request"}'
    exit 0
fi

tool_key="${request_fields[1]}"
recipe_key="${request_fields[2]}"
target_url="${request_fields[3]}"
label="${request_fields[4]}"
safe_label="${request_fields[5]}"
screenshot_path="${request_fields[6]}"
wait_ms="${request_fields[7]}"
headless="${request_fields[8]}"
allowed_domain="${request_fields[9]}"

case "${recipe_key,,}" in
    *submit*|*post*|*publish*|*delete*|*buy*|*sell*|*transfer*|*like*|*follow*|*message*)
        emit_json "blocked" "${tool_key}" "DOM fast lane rejected mutation-shaped recipe ${recipe_key}." "$(blocked_json "unsupported_mutation" "${recipe_key}" "${target_url}")"
        exit 0
        ;;
esac

if [[ "${recipe_key}" != "fixture_status" ]]; then
    emit_json "blocked" "${tool_key}" "DOM fast lane recipe ${recipe_key} is not supported." "$(blocked_json "unsupported_recipe" "${recipe_key}" "${target_url}")"
    exit 0
fi

browser_access_sh="$(resolve_browser_access_lib)"
if [[ ! -f "${browser_access_sh}" ]]; then
    emit_json "failed" "${tool_key}" "Browser access library not found at ${browser_access_sh}." "$(python3 - "${recipe_key}" "${target_url}" "${browser_access_sh}" <<'PY'
import json
import sys

recipe_key, target_url, browser_access_sh = sys.argv[1:4]
print(json.dumps({
    "recipe_key": recipe_key,
    "target_url": target_url,
    "reason": "browser_lib_missing",
    "browser_access_path": browser_access_sh,
}, separators=(",", ":")))
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

if declare -F browser_request_domain_access >/dev/null 2>&1; then
    if ! browser_request_domain_access "${target_url}" >/dev/null 2>&1; then
        emit_json "blocked" "${tool_key}" "DOM fast lane blocked disallowed domain." "$(blocked_json "domain_changed" "${recipe_key}" "${target_url}")"
        exit 0
    fi
fi

if [[ -n "${allowed_domain}" ]]; then
    if ! python3 - "${target_url}" "${allowed_domain}" <<'PY'
import sys
from urllib.parse import urlparse

target_url, allowed_domain = sys.argv[1:3]
host = (urlparse(target_url).hostname or "").lower()
raise SystemExit(0 if host == allowed_domain else 1)
PY
    then
        emit_json "blocked" "${tool_key}" "DOM fast lane blocked domain outside approved recipe scope." "$(blocked_json "domain_changed" "${recipe_key}" "${target_url}")"
        exit 0
    fi
fi

launch_mode="--headless"
if [[ "${headless}" == "false" ]]; then
    launch_mode="--headed"
fi

if ! browser_server_health >/dev/null 2>&1; then
    if ! browser_server_start --url "${target_url}" "${launch_mode}" >/dev/null 2>&1; then
        emit_json "blocked" "${tool_key}" "DOM fast lane could not start browser." "$(blocked_json "browser_start_failed" "${recipe_key}" "${target_url}")"
        exit 0
    fi
fi

if ! browser_navigate "${target_url}" >/dev/null 2>&1; then
    emit_json "blocked" "${tool_key}" "DOM fast lane could not navigate to target URL." "$(blocked_json "domain_changed" "${recipe_key}" "${target_url}")"
    exit 0
fi

if [[ "${wait_ms}" =~ ^[0-9]+$ ]] && [[ "${wait_ms}" != "0" ]]; then
    python3 - "${wait_ms}" <<'PY'
import sys
import time

time.sleep(max(int(sys.argv[1]), 0) / 1000.0)
PY
fi

snapshot="$(browser_snapshot 2>/dev/null || true)"
challenge_reason="$(python3 - "${snapshot}" <<'PY'
import sys

text = (sys.argv[1] or "").lower()
if "sign in" in text or "password" in text or "login" in text:
    print("login_required")
elif "captcha" in text or "verify you are human" in text or "bot" in text:
    print("captcha_or_bot_check")
PY
)"
if [[ -n "${challenge_reason}" ]]; then
    emit_json "blocked" "${tool_key}" "DOM fast lane requires attended browser fallback." "$(blocked_json "${challenge_reason}" "${recipe_key}" "${target_url}" "$(python3 - "${snapshot}" <<'PY'
import json
import sys

snapshot = sys.argv[1]
print(json.dumps({"snapshot_excerpt": snapshot[:200]}, separators=(",", ":")))
PY
)")"
    exit 0
fi

extraction="$(browser_evaluate '() => {
  const rows = Array.from(document.querySelectorAll("[data-fixture-status-row], table tr")).map((row) => {
    const cells = Array.from(row.querySelectorAll("[data-name], [data-state], th, td")).map((cell) => cell.textContent.trim()).filter(Boolean);
    if (cells.length >= 2) return {name: cells[0], state: cells[1]};
    return null;
  }).filter(Boolean);
  const statusNode = document.querySelector("[data-fixture-status], [aria-label=\"status\"]");
  return {
    source_url: window.location.href,
    final_url: window.location.href,
    page_status: statusNode ? statusNode.textContent.trim() : "",
    rows,
    selector_version: "fixture-v1"
  };
}' 2>/dev/null || printf '{}')"

validated_artifacts="$(python3 - "${recipe_key}" "${target_url}" "${snapshot}" "${extraction}" <<'PY'
import json
import sys

recipe_key, target_url, snapshot, extraction_raw = sys.argv[1:5]
try:
    extraction = json.loads(extraction_raw or "{}")
except Exception:
    extraction = {}

rows = extraction.get("rows")
page_status = str(extraction.get("page_status") or "").strip()
selector_version = str(extraction.get("selector_version") or "").strip()
if not isinstance(rows, list) or not rows or not page_status or not selector_version:
    print(json.dumps({
        "ok": False,
        "artifacts": {
            "recipe_key": recipe_key,
            "target_url": target_url,
            "intervention_reason": "selector_drift",
            "snapshot_excerpt": snapshot[:200],
        },
    }, separators=(",", ":")))
    raise SystemExit(0)

source_url = str(extraction.get("source_url") or target_url).strip()
final_url = str(extraction.get("final_url") or source_url).strip()
artifacts = {
    "recipe_key": recipe_key,
    "source_url": source_url,
    "final_url": final_url,
    "page_status": page_status,
    "rows": rows,
    "data": {
        "page_status": page_status,
        "rows": rows,
    },
    "selector_version": selector_version,
    "snapshot_excerpt": snapshot[:200],
}
print(json.dumps({"ok": True, "artifacts": artifacts}, separators=(",", ":")))
PY
)"

if ! python3 - "${validated_artifacts}" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
raise SystemExit(0 if payload.get("ok") else 1)
PY
then
    artifacts_json="$(python3 - "${validated_artifacts}" <<'PY'
import json
import sys

print(json.dumps(json.loads(sys.argv[1])["artifacts"], separators=(",", ":")))
PY
)"
    emit_json "blocked" "${tool_key}" "DOM fast lane selector drift blocked fixture extraction." "${artifacts_json}"
    exit 0
fi

if [[ -z "${screenshot_path}" ]]; then
    screenshot_root="${ODIN_DIR:-${ODIN_ROOT:-${HOME}/.odin}}/browser-state/dom-fast-lane"
    mkdir -p "${screenshot_root}"
    screenshot_path="${screenshot_root}/${safe_label}.png"
fi

captured_screenshot="$(browser_bc_screenshot --output "${screenshot_path}" 2>/dev/null || true)"
if [[ -z "${captured_screenshot}" ]]; then
    captured_screenshot="${screenshot_path}"
fi

artifacts_json="$(python3 - "${validated_artifacts}" "${captured_screenshot}" "${label}" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
screenshot_path, label = sys.argv[2:4]
artifacts = payload["artifacts"]
artifacts["screenshot_path"] = screenshot_path
artifacts["label"] = label
print(json.dumps(artifacts, separators=(",", ":")))
PY
)"

emit_json "completed" "${tool_key}" "Extracted fixture status table." "${artifacts_json}"
