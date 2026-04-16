#!/usr/bin/env bash
set -euo pipefail

if [[ -n "${ODIN_CODEX_DRIVER_COMMAND:-}" ]]; then
  exec bash -lc "$ODIN_CODEX_DRIVER_COMMAND"
fi

payload="$(cat)"

if [[ -n "${ODIN_CODEX_DRIVER_TRACE:-}" ]]; then
  printf '%s\n' "$payload" >"${ODIN_CODEX_DRIVER_TRACE}"
fi

PAYLOAD="$payload" ODIN_CODEX_DRIVER_ACTION="${ODIN_CODEX_DRIVER_ACTION:-}" python3 - <<'PY'
import json
import os
import sys

payload = os.environ.get("PAYLOAD", "").strip()
request = json.loads(payload) if payload else {}
action = request.get("action") or os.environ.get("ODIN_CODEX_DRIVER_ACTION", "").strip() or "run"

if action == "health":
    response = json.loads(os.environ.get("ODIN_CODEX_DRIVER_HEALTH_RESPONSE", '{"status":"healthy","details":"fixture codex driver healthy"}'))
elif action == "run":
    response = json.loads(os.environ.get("ODIN_CODEX_DRIVER_RUN_RESPONSE", '{"status":"completed","output":"fixture codex driver"}'))
else:
    response = {
        "status": "unavailable",
        "details": f"unknown action: {action}",
    }

metadata = response.setdefault("metadata", {})
metadata.setdefault("driver", "codex_headless_script")
metadata.setdefault("mode", "fixture")
metadata.setdefault("executor_class", "plan_backed_cli")
json.dump(response, sys.stdout)
PY
