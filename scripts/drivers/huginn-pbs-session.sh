#!/usr/bin/env bash
set -euo pipefail

driver_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
default_browser_lib="${driver_dir}/lib/browser-access.sh"

resolve_browser_lib() {
    if [[ -n "${ODIN_BROWSER_ACCESS_LIB_PATH:-}" ]]; then
        printf '%s\n' "${ODIN_BROWSER_ACCESS_LIB_PATH}"
        return 0
    fi
    printf '%s\n' "${default_browser_lib}"
}

emit_json() {
    local status="$1"
    local tool_key="$2"
    local summary="$3"
    local artifacts_json="${4-}"

    if [[ -z "${artifacts_json}" ]]; then
        artifacts_json='{}'
    fi

    python3 - "$status" "$tool_key" "$summary" "$artifacts_json" <<'PY'
import json
import sys

status, tool_key, summary, artifacts_raw = sys.argv[1:5]
print(json.dumps({
    "status": status,
    "tool_key": tool_key,
    "summary": summary,
    "artifacts": json.loads(artifacts_raw),
}))
PY
}

request_json="$(cat)"

mapfile -t request_fields < <(REQUEST_JSON="${request_json}" python3 - <<'PY'
import json
import os

try:
    request = json.loads(os.environ["REQUEST_JSON"] or "{}")
except Exception as exc:
    print("error")
    print(f"invalid request json: {exc}")
    raise SystemExit(0)

tool_key = str(request.get("tool_key") or "huginn_pbs_session").strip() or "huginn_pbs_session"
payload = request.get("input") or {}
bid_period = str(payload.get("bid_period") or "").strip()
workflow_key = str(payload.get("workflow_key") or "").strip()
timezone = str(payload.get("timezone") or "America/Chicago").strip() or "America/Chicago"

if not bid_period:
    print("error")
    print("bid_period is required")
    raise SystemExit(0)
if not workflow_key:
    print("error")
    print("workflow_key is required")
    raise SystemExit(0)

print("ok")
print(tool_key)
print(bid_period)
print(workflow_key)
print(timezone)
PY
)

if [[ "${request_fields[0]:-error}" != "ok" ]]; then
    emit_json "failed" "huginn_pbs_session" "${request_fields[1]:-invalid request}" '{"reason":"invalid_request"}'
    exit 0
fi

tool_key="${request_fields[1]}"
bid_period="${request_fields[2]}"
workflow_key="${request_fields[3]}"
timezone="${request_fields[4]}"
browser_lib="$(resolve_browser_lib)"
service_name="${ODIN_HUGINN_PBS_SERVICE:-flica}"
target_url="${ODIN_HUGINN_PBS_URL:-https://jia.flica.net/online/mainmenu.cgi}"

if [[ ! -f "${browser_lib}" ]]; then
    emit_json "failed" "${tool_key}" "Browser access library not found at ${browser_lib}." "$(python3 - "${bid_period}" "${workflow_key}" "${browser_lib}" <<'PY'
import json
import sys

bid_period, workflow_key, browser_lib = sys.argv[1:4]
print(json.dumps({
    "bid_period": bid_period,
    "workflow_key": workflow_key,
    "session_state": "unavailable",
    "reason": "browser_lib_missing",
    "browser_lib_path": browser_lib,
}))
PY
)"
    exit 0
fi

# shellcheck source=/dev/null
source "${browser_lib}"

cleanup() {
    browser_server_stop >/dev/null 2>&1 || true
}
trap cleanup EXIT

session_id="$(_ba_session_handle "${service_name}" 2>/dev/null || printf '%s' "${service_name}")"

if ! browser_server_start --headless >/dev/null 2>&1; then
    emit_json "failed" "${tool_key}" "Unable to start Huginn browser server." "$(python3 - "${bid_period}" "${workflow_key}" "${session_id}" <<'PY'
import json
import sys

bid_period, workflow_key, session_id = sys.argv[1:4]
print(json.dumps({
    "bid_period": bid_period,
    "workflow_key": workflow_key,
    "session_state": "unavailable",
    "session_id": session_id,
    "evidence": ["server_start_failed"],
}))
PY
)"
    exit 0
fi

if ! browser_load_session "${service_name}" >/dev/null 2>&1; then
    emit_json "failed" "${tool_key}" "No reusable Huginn session is available for ${service_name}." "$(python3 - "${bid_period}" "${workflow_key}" "${session_id}" <<'PY'
import json
import sys

bid_period, workflow_key, session_id = sys.argv[1:4]
print(json.dumps({
    "bid_period": bid_period,
    "workflow_key": workflow_key,
    "session_state": "missing_session",
    "session_id": session_id,
    "evidence": ["session_missing"],
}))
PY
)"
    exit 0
fi

if ! browser_navigate "${target_url}" >/dev/null 2>&1; then
    emit_json "failed" "${tool_key}" "Unable to navigate the Huginn PBS session to ${target_url}." "$(python3 - "${bid_period}" "${workflow_key}" "${session_id}" <<'PY'
import json
import sys

bid_period, workflow_key, session_id = sys.argv[1:4]
print(json.dumps({
    "bid_period": bid_period,
    "workflow_key": workflow_key,
    "session_state": "navigation_failed",
    "session_id": session_id,
    "evidence": ["session_loaded", "navigation_failed"],
}))
PY
)"
    exit 0
fi

set +e
health_json="$(_bc_curl "${BROWSER_SERVER_URL}/health" 2>/dev/null)"
health_status=$?
set -e

if [[ ${health_status} -ne 0 || -z "${health_json}" ]]; then
    emit_json "failed" "${tool_key}" "Huginn browser health check failed for the PBS workflow." "$(python3 - "${bid_period}" "${workflow_key}" "${session_id}" <<'PY'
import json
import sys

bid_period, workflow_key, session_id = sys.argv[1:4]
print(json.dumps({
    "bid_period": bid_period,
    "workflow_key": workflow_key,
    "session_state": "unavailable",
    "session_id": session_id,
    "evidence": ["session_loaded", "healthcheck_failed"],
}))
PY
)"
    exit 0
fi

snapshot_text="$(browser_snapshot --compact 2>/dev/null || true)"

ODIN_HUGINN_HEALTH_JSON="${health_json}" ODIN_HUGINN_SNAPSHOT_TEXT="${snapshot_text}" python3 - "${tool_key}" "${bid_period}" "${workflow_key}" "${session_id}" <<'PY'
import json
import os
import sys

tool_key, bid_period, workflow_key, session_id = sys.argv[1:5]
snapshot = os.environ.get("ODIN_HUGINN_SNAPSHOT_TEXT", "")

try:
    health = json.loads(os.environ.get("ODIN_HUGINN_HEALTH_JSON", ""))
except Exception as exc:
    print(json.dumps({
        "status": "failed",
        "tool_key": tool_key,
        "summary": f"Unable to decode Huginn health response: {exc}",
        "artifacts": {
            "bid_period": bid_period,
            "workflow_key": workflow_key,
            "session_state": "unavailable",
            "session_id": session_id,
            "evidence": ["invalid_health_response"],
        },
    }))
    raise SystemExit(0)

browser_ok = bool(health.get("browser"))
page_ok = bool(health.get("page"))
current_url = str(health.get("url") or "")
lower_url = current_url.lower()
lower_snapshot = snapshot.lower()

login_markers = (
    "pfloginapp",
    "authorization.oauth2",
    "idp.aa.com",
    "login",
)
login_text_markers = (
    "sign in",
    "log in",
    "password",
    "employee id",
)

if not browser_ok or not page_ok:
    status = "failed"
    session_state = "unavailable"
    summary = f"Huginn session is not ready for the {bid_period} PBS workflow."
    evidence = ["server_healthy" if browser_ok else "server_unhealthy"]
elif any(marker in lower_url for marker in login_markers) or any(marker in lower_snapshot for marker in login_text_markers):
    status = "failed"
    session_state = "login_required"
    summary = f"Huginn session requires login before the {bid_period} PBS workflow can run."
    evidence = ["server_healthy", "session_loaded", "login_required"]
else:
    status = "completed"
    session_state = "ready"
    summary = f"Validated Huginn session state for the {bid_period} PBS workflow."
    evidence = ["server_healthy", "session_loaded", "window_open"]

print(json.dumps({
    "status": status,
    "tool_key": tool_key,
    "summary": summary,
    "artifacts": {
        "bid_period": bid_period,
        "workflow_key": workflow_key,
        "session_state": session_state,
        "session_id": session_id,
        "evidence": evidence,
    },
}))
PY
