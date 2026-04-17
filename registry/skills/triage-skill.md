---
kind: skill
key: triage-skill
title: Triage Skill
summary: Guides Odin through intake classification before deeper work starts.
status: active
version: "1.0.0"
enabled: true
tags:
  - intake
owners:
  - odin-core
strictness: rigid
applies_to:
  - intake
  - planning
scopes:
  - global
  - odin-core
  - project
permissions:
  - repo.read
handler_type: command
handler_ref: scripts/skills/triage-skill.sh
timeout_seconds: 15
input_schema:
  type: object
  properties:
    request:
      type: string
    scope:
      type: string
output_schema:
  type: object
  properties:
    classification:
      type: string
    next_step:
      type: string
---

# Triage Skill

## Purpose
Define a reusable skill for turning a raw request into a clear next action with bounded assumptions.

## When to Use
Use this skill when a request has arrived but the runtime still needs to decide whether to answer, plan, research, or execute.

## Inputs
The skill expects the user request, current scope, relevant repo context, and any active constraints already known to Odin.

## Procedure
Read the request, identify what the user is asking for, confirm the active scope, isolate missing facts, and state the recommended next step with minimal ambiguity.

## Outputs
The output is a concise classification, any blocking questions or assumptions, and the next runtime action Odin should take.

## Constraints
Do not skip authority checks, do not expand scope beyond the current phase, and do not preload irrelevant tools or skills.

## Success Criteria
Another worker or executor can act on the skill output without needing to reinterpret the original intake request.
