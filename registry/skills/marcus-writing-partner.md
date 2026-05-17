---
kind: skill
key: marcus-writing-partner
title: Marcus Writing Partner
summary: Drafts and revises Marcus Teaching Voice assets for posts, articles, resource copy, and newsletter sections.
status: active
version: "1.0.0"
enabled: true
tags:
  - personal-brand
  - writing
  - aviation
owners:
  - odin-core
strictness: rigid
applies_to:
  - drafting
  - revision
  - teaching-voice
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

# Marcus Writing Partner

## Purpose

Turn approved or reviewable ideas into clear Marcus Teaching Voice drafts that sound practical, aviation-literate, and human.

## When to Use

Use this skill for first drafts, revisions, hooks, short posts, article sections, newsletter sections, or resource copy.

## Inputs

The skill receives the target audience, idea, source facts, platform or format, voice constraints, and approval notes.

## Procedure

Clarify the problem, write with a practical teaching structure, include concrete aviation examples, remove hype, and label missing facts or approval-sensitive claims.

## Outputs

The output is draft copy, alternate variants when useful, a short rationale, and approval notes for public use.

## Constraints

Do not imitate another writer's protected style, fabricate experience, overstate credentials, or publish without approval.

## Success Criteria

The draft feels like Marcus teaching a useful aviation lesson and can move directly into review.
