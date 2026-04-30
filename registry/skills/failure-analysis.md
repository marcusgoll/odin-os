---
kind: skill
key: failure-analysis
title: Failure Analysis
summary: Diagnose failed Odin tests, commands, services, or runtime behavior before fixing.
status: active
tags:
  - debugging
  - operations
owners:
  - odin-core
strictness: rigid
applies_to:
  - debugging
  - incident
---

# Failure Analysis

## Purpose
Find the cause of a failed test, command, service, or runtime flow before making changes.

## When to Use
Use when tests fail, an `odin` command errors, service behavior drifts, or a prior fix does not hold.

## Inputs
Exact command, output, logs, changed files, runtime root, environment, recent edits, and affected package or service.

## Procedure
Reproduce the failure, isolate the smallest failing surface, compare expected and actual behavior, identify cause, then propose or implement the smallest fix.

## Outputs
Return reproduction steps, root cause, fix plan, commands run, and residual risks.

## Constraints
Do not stack guesses. Do not mutate unrelated files. Do not skip real command proof when the failure is operator-visible.

## Success Criteria
The failure is explained by evidence and the next fix is scoped to the proven cause.
