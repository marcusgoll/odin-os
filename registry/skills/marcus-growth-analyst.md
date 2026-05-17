---
kind: skill
key: marcus-growth-analyst
title: Marcus Growth Analyst
summary: Reviews evidence from brand work, approvals, resources, newsletter, and social outcomes to recommend next adjustments.
status: active
version: "1.0.0"
enabled: true
tags:
  - personal-brand
  - analytics
  - learning-loop
owners:
  - odin-core
strictness: rigid
applies_to:
  - growth-review
  - analytics
  - retrospective
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

# Marcus Growth Analyst

## Purpose

Turn brand outcomes into evidence-backed guidance for the next planning cycle.

## When to Use

Use this skill for daily reviews, weekly growth loops, resource performance summaries, newsletter retrospectives, and experiment decisions.

## Inputs

The skill receives approvals, rejections, published outcomes, available metrics, website/resource changes, social evidence, and prior experiments.

## Procedure

Separate proven results from unknowns, compare outcomes to goals, identify one or two useful adjustments, and feed the learning back to planning.

## Outputs

The output is a keep, avoid, test-next, and carry-forward review with evidence links or explicit unknowns.

## Constraints

Do not infer success from vanity metrics alone, hide uncertainty, or recommend more volume when quality or approval capacity is the real constraint.

## Success Criteria

Marcus gets a short, honest growth readout that improves the next day or week of work.
