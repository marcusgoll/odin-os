#!/usr/bin/env bash
set -euo pipefail

request_file="$(mktemp)"
cleanup() {
    rm -f "${request_file}"
}
trap cleanup EXIT

cat >"${request_file}"

python3 - "${request_file}" <<'PY'
import json
import os
import subprocess
import sys
from datetime import datetime, timezone

MARKER_PATH = ".odin/sandcastle-fixture-marker.json"


def finish(status, output, metadata=None, details=""):
    print(
        json.dumps(
            {
                "status": status,
                "details": details,
                "output": output,
                "metadata": metadata or {},
                "handle": {
                    "executor_key": "sandcastle_headless",
                    "external_id": (metadata or {}).get("external_id", ""),
                    "status": status,
                },
            },
            sort_keys=True,
        )
    )


request_path = sys.argv[1]
with open(request_path, "r", encoding="utf-8") as handle:
    request = json.load(handle)

action = (request.get("action") or "").strip()
if action == "health":
    finish(
        "healthy",
        "",
        {
            "driver_kind": "fixture",
            "operation": "health",
        },
        "fixture sandcastle driver healthy",
    )
    raise SystemExit(0)

task = request.get("task") or {}
metadata = task.get("metadata") or task.get("Metadata") or request.get("meta") or {}
task_id = task.get("id") or task.get("ID") or ""
worktree_path = metadata.get("worktree_path") or ""
branch_name = metadata.get("branch_name") or ""
repo_root = metadata.get("repo_root") or ""
driver_cwd = os.getcwd()

evidence = {
    "driver_kind": "fixture",
    "operation": action or "run",
    "driver_cwd": driver_cwd,
    "marker_path": MARKER_PATH,
}

if task_id:
    evidence["external_id"] = f"sandcastle-fixture:{task_id}"

if action != "run":
    finish("failed", f"unsupported sandcastle fixture action: {action}", evidence)
    raise SystemExit(0)
if not worktree_path:
    finish("failed", "sandcastle fixture missing worktree_path", evidence)
    raise SystemExit(0)
if not branch_name:
    finish("failed", "sandcastle fixture missing branch_name", evidence)
    raise SystemExit(0)
if not os.path.isdir(worktree_path):
    finish("failed", f"sandcastle fixture worktree does not exist: {worktree_path}", evidence)
    raise SystemExit(0)
if os.path.realpath(driver_cwd) != os.path.realpath(worktree_path):
    finish("failed", f"sandcastle fixture cwd {driver_cwd} did not match worktree {worktree_path}", evidence)
    raise SystemExit(0)

branch = subprocess.check_output(
    ["git", "-C", worktree_path, "branch", "--show-current"],
    text=True,
).strip()
evidence["branch_observed"] = branch
if branch != branch_name:
    finish("failed", f"sandcastle fixture branch {branch} did not match {branch_name}", evidence)
    raise SystemExit(0)

marker_abs = os.path.join(worktree_path, MARKER_PATH)
os.makedirs(os.path.dirname(marker_abs), exist_ok=True)
with open(marker_abs, "w", encoding="utf-8") as marker:
    json.dump(
        {
            "task_id": task_id,
            "operation": action,
            "driver_kind": "fixture",
            "driver_cwd": driver_cwd,
            "repo_root": repo_root,
            "worktree_path": worktree_path,
            "branch_name": branch_name,
            "branch_observed": branch,
            "created_at": datetime.now(timezone.utc).isoformat(),
        },
        marker,
        sort_keys=True,
    )
    marker.write("\n")

evidence["marker_written"] = "true"
finish("completed", f"sandcastle fixture completed {task_id} on {branch}", evidence)
PY
