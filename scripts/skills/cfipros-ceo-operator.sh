#!/usr/bin/env bash
set -euo pipefail

payload="$(cat)"
python3 -c '
import json
import sys

raw = sys.stdin.read()
try:
    data = json.loads(raw) if raw.strip() else {}
except json.JSONDecodeError:
    data = {}

input_data = data.get("input") or {}
agent_key = input_data.get("agent_key") or "cfipros-ceo-operator-agent"
workflow_key = input_data.get("workflow_key") or "cfipros-ceo-operating-routine"
checkpoint = input_data.get("checkpoint") or "unspecified"
approval_boundary = input_data.get("approval_boundary") or "external actions require explicit human approval"
external_side_effect = input_data.get("external_side_effect") or "none"
project_key = input_data.get("project_key") or "cfipros"

output = {
    "result": "cfipros_ceo_operator_handoff_ready",
    "agent_key": agent_key,
    "workflow_key": workflow_key,
    "checkpoint": checkpoint,
    "project_key": project_key,
    "approval_required": True,
    "external_side_effect": external_side_effect,
    "approval_boundary": approval_boundary,
}

print(json.dumps({
    "skill_key": "cfipros-ceo-operator",
    "status": "ok",
    "summary": f"CFIPros CEO operator handoff ready: {checkpoint}",
    "output": output,
    "raw_output": json.dumps(output, sort_keys=True),
}, sort_keys=True))
' <<<"${payload}"
