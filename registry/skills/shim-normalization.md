---
kind: skill
key: shim-normalization
title: Shim Normalization
summary: Wrap shim behavior behind typed Go interfaces before retirement.
status: active
tags:
  - shims
  - migration
owners:
  - odin-core
strictness: rigid
applies_to:
  - shims
  - refactor
---

# Shim Normalization

## Purpose
Convert shell, placeholder, or duplicate shim behavior into typed adapter boundaries while preserving compatibility.

## When to Use
Use when work touches scripts, thin wrappers, duplicate Go seams, placeholder adapters, or migration tooling.

## Inputs
Shim inventory, current callers, compatibility requirements, target interface, security risks, and characterization tests.

## Procedure
Pick one high-value shim, add tests around current behavior, introduce a typed adapter path, keep old calls available, and document retirement conditions.

## Outputs
Return the adapter interface, compatibility behavior, tests, preserved shim path, and retirement plan.

## Constraints
Do not delete shims in the normalization slice. Do not move files without a migration reason. Security-sensitive shims require review.

## Success Criteria
Existing behavior still works and future callers have a typed path that can replace the shim later.
