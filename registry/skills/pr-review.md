---
kind: skill
key: pr-review
title: PR Review
summary: Review pull-request readiness against Odin's template, tests, and human handoff rules.
status: active
tags:
  - review
  - pull-request
owners:
  - odin-core
strictness: review
applies_to:
  - review
  - pull-request
---

# PR Review

## Purpose
Assess whether a change is ready for human review without implying autonomous merge authority.

## When to Use
Use before opening, updating, or closing out a PR, especially when the change affects operator-visible behavior.

## Inputs
Diff, PR template, commands run, tests, real `odin` proof, known risks, and unproven behavior.

## Procedure
Prioritize bugs, regressions, missing tests, template violations, undocumented risks, and unclear handoff evidence.

## Outputs
Return findings first, then proof summary, unproven areas, and required PR body content.

## Constraints
Do not merge autonomously. Do not claim completion without command evidence matching the change surface.

## Success Criteria
The PR handoff is truthful, reviewable, and compatible with Odin's required headings and proof standards.
