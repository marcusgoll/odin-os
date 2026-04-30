---
kind: skill
key: qa-review
title: QA Review
summary: Plan and run verification for Odin changes without granting merge authority.
status: active
tags:
  - qa
  - verification
owners:
  - odin-core
strictness: review
applies_to:
  - qa
  - verification
---

# QA Review

## Purpose
Create focused verification evidence for code, docs, registry, and operator-surface changes.

## When to Use
Use after implementation or during review when test coverage, command proof, or runtime evidence is incomplete.

## Inputs
Changed files, acceptance criteria, existing tests, relevant commands, runtime prerequisites, and known unproven areas.

## Procedure
Map risks to tests, run the narrow checks first, then broader gates. Include real `odin` proof for user-visible behavior.

## Outputs
Return commands run, results, failures, unproven behavior, and recommended next checks.

## Constraints
QA evidence informs human review but does not approve merge or production deployment.

## Success Criteria
The operator can see what passed, what failed, and what remains unproven.
