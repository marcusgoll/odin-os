#!/usr/bin/env python3

import json
import os
import sqlite3
import sys


def main() -> int:
    spec = json.load(sys.stdin)
    runtime_root = spec.get("runtime_root", "")
    args = spec.get("args") or {}
    project_key = args.get("project_key") or "current"

    if not runtime_root:
        print("runtime_root is required", file=sys.stderr)
        return 1

    db_path = os.path.join(runtime_root, "data", "odin.db")
    if not os.path.exists(db_path):
        print(f"runtime database not found: {db_path}", file=sys.stderr)
        return 1

    conn = sqlite3.connect(db_path)
    try:
        conn.row_factory = sqlite3.Row
        project = conn.execute(
            """
            SELECT id, key, name, scope
            FROM projects
            WHERE key = ?
            """,
            (project_key,),
        ).fetchone()
        if project is None:
            print(f"project not found: {project_key}", file=sys.stderr)
            return 1

        open_task_count = conn.execute(
            """
            SELECT COUNT(*)
            FROM tasks
            WHERE project_id = ?
              AND status NOT IN ('completed', 'cancelled', 'dead_letter')
            """,
            (project["id"],),
        ).fetchone()[0]

        payload = {
            "source": "driver",
            "summary": f"Project {project['key']} has {open_task_count} open task(s).",
            "key_facts": {
                "project_key": project["key"],
                "project_name": project["name"],
                "project_scope": project["scope"],
                "open_task_count": str(open_task_count),
            },
            "follow_on_options": ["inspect tasks", "open project portfolio"],
            "raw_ref": f"driver://project_status/{project['key']}",
            "raw_output": f"project={project['key']} open_tasks={open_task_count}",
        }
        json.dump(payload, sys.stdout)
        return 0
    finally:
        conn.close()


if __name__ == "__main__":
    raise SystemExit(main())
