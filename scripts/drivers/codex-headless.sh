#!/usr/bin/env bash
set -euo pipefail

if [[ -n "${ODIN_CODEX_DRIVER_COMMAND:-}" ]]; then
  exec bash -lc "$ODIN_CODEX_DRIVER_COMMAND"
fi

python3 -c '
import json
import os
import sys

mode = os.environ.get("ODIN_CODEX_DRIVER_MODE", "fixture")
if mode == "fail":
    print("codex headless driver forced failure", file=sys.stderr)
    sys.exit(1)

spec = json.load(sys.stdin)
task_id = spec.get("id", "")
kind = spec.get("kind", "general")
scope = spec.get("scope", "project")
prompt = spec.get("prompt", "")

payload = {
    "status": "completed",
    "output": f"codex_headless_script completed {kind} task {task_id} in {scope} scope: {prompt}",
    "metadata": {
        "driver": "codex_headless_script",
        "mode": mode,
        "executor_class": "plan_backed_cli",
    },
}
json.dump(payload, sys.stdout)
'
