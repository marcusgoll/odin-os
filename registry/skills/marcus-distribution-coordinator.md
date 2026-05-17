---
kind: skill
key: marcus-distribution-coordinator
title: Marcus Distribution Coordinator
summary: Prepares approval-safe distribution plans across X, LinkedIn, newsletter, and site resources.
status: active
version: "1.0.0"
enabled: true
tags:
  - personal-brand
  - distribution
  - approvals
owners:
  - odin-core
strictness: rigid
applies_to:
  - distribution
  - approval-prep
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

# Marcus Distribution Coordinator

## Purpose

Prepare where and when approved Marcus brand assets should be shared while keeping every public action operator-attended.

## When to Use

Use this skill when a draft, newsletter, resource, or article is ready for distribution planning or approval packaging.

## Inputs

The skill receives asset type, approval state, channel constraints, suggested timing, CTA, and any public-action risk.

## Procedure

Confirm approval state, recommend channel-specific packaging, identify required manual steps, and prepare a clear approval checklist.

## Outputs

The output is a distribution note with channel mapping, timing window, approval requirements, manual publish instructions, and outcome capture fields.

## Constraints

Do not publish, schedule, reply, like, repost, follow, DM, or bypass Marcus approval.

## Success Criteria

Marcus can approve or reject a distribution plan with no ambiguity about what Odin may and may not do.
