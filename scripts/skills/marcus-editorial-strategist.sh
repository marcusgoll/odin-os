#!/usr/bin/env bash
set -euo pipefail

payload="$(cat)"
python3 -c '
import json
import re
import sys

raw = sys.stdin.read()
try:
    data = json.loads(raw) if raw.strip() else {}
except json.JSONDecodeError:
    data = {}

input_data = data.get("input") or {}
context = data.get("context") or {}
project = context.get("project") or {}

request = str(input_data.get("request") or "").strip()
workflow_key = str(input_data.get("workflow_key") or "marcus-personal-brand-operating-system").strip()
source = str(input_data.get("source") or "manual-skill-invocation").strip()
approval_boundary = str(input_data.get("approval_boundary") or "internal drafting and analysis only; public actions require Marcus approval").strip()
project_key = str(input_data.get("project_key") or project.get("key") or "marcusgoll").strip()

normalized = request.lower()
tokens = set(re.findall(r"[a-z0-9]+", normalized))

def has_any(*words):
    return any(word in tokens or word in normalized for word in words)

if has_any("morning", "editorial scan", "priorities", "draft next"):
    route = "marcus-writing-partner"
    lane = "writing"
elif has_any("resource production", "lead-magnet", "checklist", "guide", "tool"):
    route = "marcus-resource-producer"
    lane = "resource"
elif has_any("newsletter", "email"):
    route = "marcus-newsletter-editor"
    lane = "newsletter"
elif has_any("growth", "outcomes", "analytics", "review"):
    route = "marcus-growth-analyst"
    lane = "growth"
else:
    route = "marcus-writing-partner"
    lane = "writing"

editorial_priorities = [
    {
        "rank": 1,
        "lane": lane,
        "title": "Choose one practical aviation teaching problem Marcus can address today",
        "rationale": "The request asks for a morning editorial scan; the strongest next move is a narrow, useful topic tied to pilot progression, CFI judgment, or training clarity.",
        "recommended_next_skill": route,
        "approval_sensitivity": "internal_strategy_only",
    },
    {
        "rank": 2,
        "lane": "writing",
        "title": "Convert the selected problem into one Marcus Teaching Voice draft",
        "rationale": "A reviewable draft gives Marcus something concrete to approve, reject, or reshape without taking public account actions.",
        "recommended_next_skill": "marcus-writing-partner",
        "approval_sensitivity": "public_copy_requires_marcus_approval",
    },
    {
        "rank": 3,
        "lane": "growth",
        "title": "Capture what evidence is missing before expanding the idea",
        "rationale": "The brand loop should label unknowns instead of inventing performance claims or time-sensitive aviation facts.",
        "recommended_next_skill": "marcus-growth-analyst",
        "approval_sensitivity": "analysis_only",
    },
]

missing_facts = [
    "current approved Marcus brand priorities from marcusgoll",
    "recent social/content outcomes from the approval-gated social loop",
    "pending drafts or resource assets already awaiting Marcus review",
]

no_go_topics = [
    "unverified aviation safety, regulation, training, or airline-career claims",
    "fake urgency, engagement bait, or claims that imply outcomes not backed by evidence",
    "publishing, replying, sending email, account actions, or scheduling public content without explicit Marcus approval",
]

output = {
    "result": "marcus_editorial_strategy_ready",
    "workflow_key": workflow_key,
    "project_key": project_key,
    "source": source,
    "source_request": request,
    "editorial_priorities": editorial_priorities,
    "recommended_next_skill": route,
    "approval_required": True,
    "approval_boundary": approval_boundary,
    "public_action_allowed": False,
    "external_side_effect": "none",
    "missing_facts": missing_facts,
    "no_go_topics": no_go_topics,
    "review_note": "Use this as the reviewable morning strategy handoff before any downstream drafting or public action.",
}

print(json.dumps({
    "skill_key": "marcus-editorial-strategist",
    "status": "ok",
    "summary": f"Marcus editorial strategy ready: {len(editorial_priorities)} priorities, next skill {route}",
    "output": output,
    "raw_output": json.dumps(output, sort_keys=True),
}, sort_keys=True))
' <<<"${payload}"
