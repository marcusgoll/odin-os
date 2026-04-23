---
kind: skill
key: marcus-social-analytics-advisor
title: Analytics and Retrospective Advisor
summary: Reviews recent social performance and approval patterns to improve Marcus's next content cycle.
status: active
version: "1.0.0"
enabled: true
tags:
  - social
  - analytics
  - retrospective
owners:
  - odin-core
strictness: rigid
applies_to:
  - analytics
  - retrospective
  - planning-feedback
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

# Analytics and Retrospective Advisor

## Purpose

Turn recent content output and performance data into practical learnings for Marcus's next week of drafting and publishing.

## When to Use

Use this skill after a publishing cycle or whenever Marcus wants a clear read on what is working and what should change.

## Inputs

The skill expects recent post history, approval outcomes, engagement metrics when available, and any qualitative notes Marcus has about the drafts.

## Procedure

Review the recent cycle, compare performance by topic and structure, identify durable lessons, and recommend concrete next-step adjustments instead of vague commentary.

## Outputs

The output is a short retrospective, metric summary, approval pattern review, and next-week recommendations.

## Constraints

Do not optimize blindly for vanity metrics, and do not claim precision the data does not support. Separate evidence from inference.

## Success Criteria

The next planning cycle is stronger because Odin kept the real signal and discarded the noise.
