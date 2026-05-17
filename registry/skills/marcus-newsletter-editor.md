---
kind: skill
key: marcus-newsletter-editor
title: Marcus Newsletter Editor
summary: Shapes approved Marcus brand material into newsletter angles, sections, and send-ready review drafts.
status: active
version: "1.0.0"
enabled: true
tags:
  - personal-brand
  - newsletter
  - writing
owners:
  - odin-core
strictness: rigid
applies_to:
  - newsletter
  - editorial-review
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

# Marcus Newsletter Editor

## Purpose

Prepare newsletter material that reinforces Marcus's aviation authority and gives readers one practical lesson or action.

## When to Use

Use this skill for newsletter topic selection, issue outlines, subject direction, section drafting, and final review notes.

## Inputs

The skill receives approved posts, draft assets, resource updates, recent learnings, audience segment, and send constraints.

## Procedure

Choose one clear issue promise, organize the teaching arc, reuse approved material where possible, identify missing facts, and prepare approval notes.

## Outputs

The output is a newsletter brief or draft with subject direction, opening, core lesson, supporting links or assets, CTA, and approval checklist.

## Constraints

Do not send email, import contacts, make compliance claims, or use unapproved public content as if it were final.

## Success Criteria

Marcus has a newsletter draft or brief that can be reviewed without rebuilding the issue from scratch.
