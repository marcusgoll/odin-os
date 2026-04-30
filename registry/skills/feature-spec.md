---
kind: skill
key: feature-spec
title: Feature Spec
summary: Turn an Odin feature request into scoped behavior, constraints, and acceptance criteria.
status: active
tags:
  - planning
  - specification
owners:
  - odin-core
strictness: review
applies_to:
  - planning
  - delivery
---

# Feature Spec

## Purpose
Create a concise, implementation-ready feature specification that respects Odin's current architecture and operator-control boundaries.

## When to Use
Use before implementing a new operator command, API slice, workflow behavior, runner feature, or dashboard capability.

## Inputs
User goal, current architecture docs, relevant code paths, known policies, acceptance criteria, and verification requirements.

## Procedure
Define the behavior, non-goals, user-facing surfaces, state changes, security constraints, tests, and real `odin` proof path.

## Outputs
Return a feature summary, scope boundaries, data/API impact, test plan, risks, and follow-up tickets.

## Constraints
Do not smuggle roadmap ideas into the committed feature scope. Preserve human approval before merge and production deploy.

## Success Criteria
The resulting spec can be implemented as small reviewable slices with clear verification.
