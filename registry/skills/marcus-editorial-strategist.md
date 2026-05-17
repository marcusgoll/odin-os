---
kind: skill
key: marcus-editorial-strategist
title: Marcus Editorial Strategist
summary: Chooses the strongest daily Marcus brand priorities from ideas, outcomes, audience needs, and aviation authority goals.
status: active
version: "1.0.0"
enabled: true
tags:
  - personal-brand
  - aviation
  - planning
owners:
  - odin-core
strictness: rigid
applies_to:
  - editorial-planning
  - content-prioritization
scopes:
  - project
permissions:
  - repo.read
handler_type: command
handler_ref: scripts/skills/registry-skill-stub.sh
timeout_seconds: 15
input_schema:
  type: object
  properties:
    request:
      type: string
output_schema:
  type: object
  properties:
    result:
      type: string
---

# Marcus Editorial Strategist

## Purpose

Select the highest-value Marcus brand priorities for a given day or week and keep the work grounded in aviation teaching authority.

## When to Use

Use this skill when Odin needs to choose what Marcus should draft, revise, package, or review next.

## Inputs

The skill receives current ideas, recent outcomes, pending approvals, resource gaps, audience needs, and any time-sensitive aviation context.

## Procedure

Review the inputs, filter out low-signal or risky angles, rank the best opportunities, and assign each opportunity to a writing, resource, newsletter, marketing, or growth lane.

## Outputs

The output is a concise priority list with recommended lane, rationale, missing facts, approval sensitivity, and the next skill to invoke.

## Constraints

Do not recommend fake urgency, engagement bait, unverified aviation claims, or public action without Marcus approval.

## Success Criteria

Marcus can quickly see what matters today and which brand lane should work on it next.
