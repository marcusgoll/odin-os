#!/usr/bin/env bash
set -euo pipefail

payload="$(cat)"

if [[ -n "${ODIN_CODEX_DRIVER_TRACE:-}" ]]; then
	printf '%s\n' "$payload" >"${ODIN_CODEX_DRIVER_TRACE}"
fi

PAYLOAD="$payload" python3 - <<'PY'
import json
import os

request = json.loads(os.environ["PAYLOAD"])
action = request.get("action")

if action == "health":
    response = {
        "status": "healthy",
        "details": "fixture codex driver healthy",
    }
elif action == "run":
    response = {
        "status": "completed",
        "output": "fixture codex driver",
    }
else:
    response = {
        "status": "unavailable",
        "details": f"unknown action: {action}",
    }

print(json.dumps(response))
PY
