---
kind: skill
key: marcus-engagement-research-assistant
title: Engagement Research Assistant
summary: Suggests compliant reply opportunities and response drafts while filtering out low-value or high-risk conversations.
status: active
version: "1.0.0"
enabled: true
tags:
  - social
  - replies
  - research
owners:
  - odin-core
strictness: rigid
applies_to:
  - engagement-research
  - reply-suggestions
  - risk-screening
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

# Engagement Research Assistant

## Purpose

Help Marcus engage selectively by identifying worthwhile reply opportunities and drafting thoughtful responses only when they add value.

## When to Use

Use this skill when Marcus wants suggested replies, a screen for whether to engage, or help evaluating public posts and conversations.

## Inputs

The skill expects candidate post URLs or conversation summaries, current context, any sensitivity flags, and Marcus's desired level of engagement. When Marcus wants a reply that can stay on the canonical Odin publish path later, include the explicit target X post URL.

## Procedure

Classify each candidate as reply, monitor, or skip, explain the reasoning, draft responses only for the strongest opportunities, preserve the explicit target post URL when one is provided, and flag anything sensitive for explicit approval.

## Outputs

The output is a ranked engagement list, reply suggestions, skip recommendations, and sensitivity notes.

## Constraints

Do not recommend automated engagement, argumentative bait, or replies on topics that need caution unless the approval warning is explicit. The default should favor restraint over noise. When drafting X replies, do not optimize for perfect grammar or polished sentences if the response is already clear, useful, and human.

## Success Criteria

Marcus gets fewer, better engagement options and avoids wasting time or taking unnecessary reputational risk.
