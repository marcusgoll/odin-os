#!/usr/bin/env bash
set -euo pipefail

driver_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
default_google_lib="${driver_dir}/lib/google.sh"

resolve_google_lib() {
    if [[ -n "${ODIN_GOOGLE_LIB_PATH:-}" ]]; then
        printf '%s\n' "${ODIN_GOOGLE_LIB_PATH}"
        return 0
    fi
    printf '%s\n' "${default_google_lib}"
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

tool_key = str(request.get("tool_key") or "google_calendar_off_dates").strip() or "google_calendar_off_dates"
payload = request.get("input") or {}
bid_period = str(payload.get("bid_period") or "").strip()
calendar_id = str(payload.get("calendar_id") or "primary").strip() or "primary"
timezone = str(payload.get("timezone") or "America/Chicago").strip() or "America/Chicago"

if not bid_period:
    print("error")
    print("bid_period is required")
    raise SystemExit(0)

parts = bid_period.split("-")
if len(parts) != 2 or len(parts[0]) != 4 or len(parts[1]) != 2:
    print("error")
    print("bid_period must be YYYY-MM")
    raise SystemExit(0)

year = int(parts[0])
month = int(parts[1])
if month < 1 or month > 12:
    print("error")
    print("bid_period month must be between 01 and 12")
    raise SystemExit(0)

print("ok")
print(tool_key)
print(bid_period)
print(calendar_id)
print(timezone)
PY
)

if [[ "${request_fields[0]:-error}" != "ok" ]]; then
    emit_json "failed" "google_calendar_off_dates" "${request_fields[1]:-invalid request}" '{"reason":"invalid_request"}'
    exit 0
fi

tool_key="${request_fields[1]}"
bid_period="${request_fields[2]}"
calendar_id="${request_fields[3]}"
timezone="${request_fields[4]}"
google_lib="$(resolve_google_lib)"

if [[ ! -f "${google_lib}" ]]; then
    emit_json "failed" "${tool_key}" "Google library not found at ${google_lib}." "$(python3 - "${bid_period}" "${calendar_id}" "${timezone}" "${google_lib}" <<'PY'
import json
import sys

bid_period, calendar_id, timezone, google_lib = sys.argv[1:5]
print(json.dumps({
    "bid_period": bid_period,
    "calendar_id": calendar_id,
    "timezone": timezone,
    "reason": "google_lib_missing",
    "google_lib_path": google_lib,
}))
PY
)"
    exit 0
fi

# shellcheck source=/dev/null
source "${google_lib}"

url="$(python3 - "${bid_period}" "${calendar_id}" "${timezone}" <<'PY'
from datetime import datetime
from urllib.parse import quote
from zoneinfo import ZoneInfo
import sys

bid_period, calendar_id, timezone_name = sys.argv[1:4]
year, month = map(int, bid_period.split("-"))
tz = ZoneInfo(timezone_name)
start = datetime(year, month, 1, 0, 0, 0, tzinfo=tz)
if month == 12:
    end = datetime(year + 1, 1, 1, 0, 0, 0, tzinfo=tz)
else:
    end = datetime(year, month + 1, 1, 0, 0, 0, tzinfo=tz)

calendar_segment = quote(calendar_id, safe="")
time_min = quote(start.isoformat(), safe="")
time_max = quote(end.isoformat(), safe="")
print(
    f"https://www.googleapis.com/calendar/v3/calendars/{calendar_segment}/events"
    f"?timeMin={time_min}&timeMax={time_max}&singleEvents=true&orderBy=startTime"
)
PY
)"

set +e
api_response="$(google_api_call GET "${url}" 2>&1)"
api_status=$?
set -e

if [[ ${api_status} -ne 0 ]]; then
    emit_json "failed" "${tool_key}" "Google Calendar request failed for ${bid_period}." "$(python3 - "${bid_period}" "${calendar_id}" "${timezone}" "${api_status}" "${api_response}" <<'PY'
import json
import sys

bid_period, calendar_id, timezone, api_status, api_response = sys.argv[1:6]
print(json.dumps({
    "bid_period": bid_period,
    "calendar_id": calendar_id,
    "timezone": timezone,
    "reason": "calendar_api_failed",
    "exit_status": int(api_status),
    "error": api_response.strip(),
}))
PY
)"
    exit 0
fi

GOOGLE_API_RESPONSE="${api_response}" python3 - "${tool_key}" "${bid_period}" "${calendar_id}" "${timezone}" <<'PY'
from datetime import date, datetime, timedelta
from zoneinfo import ZoneInfo
import json
import os
import sys

tool_key, bid_period, calendar_id, timezone_name = sys.argv[1:5]
year, month = map(int, bid_period.split("-"))
period_start = date(year, month, 1)
period_end = date(year + (month // 12), (month % 12) + 1, 1)

try:
    timezone = ZoneInfo(timezone_name)
except Exception as exc:
    print(json.dumps({
        "status": "failed",
        "tool_key": tool_key,
        "summary": f"Invalid timezone {timezone_name}: {exc}",
        "artifacts": {
            "bid_period": bid_period,
            "calendar_id": calendar_id,
            "timezone": timezone_name,
            "reason": "invalid_timezone",
        },
    }))
    raise SystemExit(0)

try:
    payload = json.loads(os.environ.get("GOOGLE_API_RESPONSE", ""))
except Exception as exc:
    print(json.dumps({
        "status": "failed",
        "tool_key": tool_key,
        "summary": f"Google Calendar returned invalid JSON: {exc}",
        "artifacts": {
            "bid_period": bid_period,
            "calendar_id": calendar_id,
            "timezone": timezone_name,
            "reason": "invalid_calendar_response",
        },
    }))
    raise SystemExit(0)

def parse_datetime(value: str) -> datetime:
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    return datetime.fromisoformat(value)

off_dates = set()

for item in payload.get("items") or []:
    start = item.get("start") or {}
    end = item.get("end") or {}
    if start.get("date"):
        start_date = date.fromisoformat(start["date"])
        end_date = date.fromisoformat(end.get("date") or start["date"])
        cursor = max(start_date, period_start)
        limit = min(end_date, period_end)
        while cursor < limit:
            off_dates.add(cursor.isoformat())
            cursor += timedelta(days=1)
        continue

    start_dt_raw = start.get("dateTime")
    if not start_dt_raw:
        continue

    start_dt = parse_datetime(start_dt_raw).astimezone(timezone)
    end_dt_raw = end.get("dateTime") or start_dt_raw
    end_dt = parse_datetime(end_dt_raw).astimezone(timezone)
    if end_dt <= start_dt:
        end_dt = start_dt + timedelta(minutes=1)

    cursor = start_dt.date()
    last_date = (end_dt - timedelta(microseconds=1)).date()
    while cursor <= last_date:
        if period_start <= cursor < period_end:
            off_dates.add(cursor.isoformat())
        cursor += timedelta(days=1)

ordered = sorted(off_dates)

print(json.dumps({
    "status": "completed",
    "tool_key": tool_key,
    "summary": f"Found {len(ordered)} off-dates for {bid_period}.",
    "artifacts": {
        "bid_period": bid_period,
        "calendar_id": calendar_id,
        "timezone": timezone_name,
        "off_dates": ordered,
    },
}))
PY
