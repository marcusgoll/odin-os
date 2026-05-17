---
kind: skill
key: marcus-resource-producer
title: Marcus Resource Producer
summary: Converts repeated audience problems into useful checklist, guide, tool, or lead-magnet candidates.
status: active
version: "1.0.0"
enabled: true
tags:
  - personal-brand
  - resources
  - conversion
owners:
  - odin-core
strictness: rigid
applies_to:
  - resource-production
  - lead-magnet-planning
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

# Marcus Resource Producer

## Purpose

Package Marcus's recurring aviation lessons into useful resources that can help an audience and support the website funnel.

## When to Use

Use this skill when an idea should become a checklist, guide, worksheet, article asset, or downloadable resource.

## Inputs

The skill receives the audience problem, source material, intended conversion path, related content, and any constraints from `marcusgoll`.

## Procedure

Define the resource promise, outline the artifact, identify required proof or examples, map it to the site journey, and propose the next production step.

## Outputs

The output is a resource brief with audience, problem, artifact shape, sections, proof needs, conversion path, and next action.

## Constraints

Do not create generic lead magnets, unsupported claims, or resources that promise outcomes Marcus cannot stand behind.

## Success Criteria

The resource brief is specific enough for Marcus or another agent to draft the asset without re-deciding the strategy.
