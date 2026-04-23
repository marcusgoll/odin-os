---
kind: skill
key: marcus-social-content-strategist
title: Content Strategist Advisor
summary: Builds a compliant weekly social plan for Marcus around aviation authority, teaching value, and realistic publishing cadence.
status: active
version: "1.0.0"
enabled: true
tags:
  - social
  - aviation
  - planning
owners:
  - odin-core
strictness: rigid
applies_to:
  - planning
  - content-calendar
  - social-strategy
scopes:
  - global
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

# Content Strategist Advisor

## Purpose

Turn Marcus's raw ideas, topic pillars, and recent learnings into a focused X and LinkedIn content plan that stays compliant and realistic.

## When to Use

Use this skill when Marcus needs a weekly plan, a content calendar, topic prioritization, or a recommendation about what should be drafted next.

## Inputs

The skill expects topic pillars, recent approved content, desired cadence, platform focus, and any time-sensitive aviation themes or events.

## Procedure

Audit the topic mix, choose the strongest angles, map ideas to X or LinkedIn, and return a compact weekly plan with clear drafting priorities and approval notes.

## Outputs

The output is a one-week content plan, draft briefs, and any gaps or sensitivity flags that should be resolved before drafting.

## Constraints

Do not recommend stealth automation, empty volume targets, or content that depends on fake urgency. Keep the plan grounded in Marcus's real expertise and approval capacity.

## Success Criteria

Marcus can immediately choose which ideas to draft because the plan is clear, useful, and realistically paced.
