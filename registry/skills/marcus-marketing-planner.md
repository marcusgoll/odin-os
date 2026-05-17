---
kind: skill
key: marcus-marketing-planner
title: Marcus Marketing Planner
summary: Connects Marcus brand assets to site journeys, resource funnels, and channel-specific promotion plans.
status: active
version: "1.0.0"
enabled: true
tags:
  - personal-brand
  - marketing
  - funnel
owners:
  - odin-core
strictness: rigid
applies_to:
  - marketing-planning
  - funnel-review
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

# Marcus Marketing Planner

## Purpose

Plan how Marcus brand assets should move from attention to trust to resource signup or deeper engagement.

## When to Use

Use this skill when a draft, resource, newsletter, or website update needs promotion logic or funnel placement.

## Inputs

The skill receives asset type, target audience, channel, conversion goal, site journey, and available proof.

## Procedure

Map the asset to a journey stage, identify the best channel, define a practical CTA, note tracking needs, and flag any mismatch with the brand authority repo.

## Outputs

The output is a short marketing plan with channel fit, CTA, placement, measurement, and follow-up recommendations.

## Constraints

Do not optimize for vanity metrics, dark patterns, exaggerated claims, or unsupported automation.

## Success Criteria

The plan makes it obvious how the asset should help the audience and what Marcus should measure next.
