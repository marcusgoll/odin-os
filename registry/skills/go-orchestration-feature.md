---
kind: skill
key: go-orchestration-feature
title: Go Orchestration Feature
summary: Implement a small Go orchestration slice through existing Odin runtime seams.
status: active
tags:
  - go
  - orchestration
owners:
  - odin-core
strictness: rigid
applies_to:
  - implementation
  - orchestration
---

# Go Orchestration Feature

## Purpose
Guide implementation of Go-based Odin orchestration features without creating sidecar runtimes or duplicate state authority.

## When to Use
Use for work touching lifecycle, bootstrap, runtime jobs, runs, recovery, executor routing, VCS leases, or dashboard/API surfaces.

## Inputs
Feature spec, current packages, tests, SQLite migrations if any, command path, and verification plan.

## Procedure
Add focused tests first, implement through existing services, keep command handlers thin, and verify through package tests plus a real `odin` command where applicable.

## Outputs
Return changed files, reused seams, new behavior, tests, real command proof, and unresolved risks.

## Constraints
Do not introduce a second runtime state store, second command authority, or worker launch path outside Odin-owned services.

## Success Criteria
The feature works through existing Odin composition and remains resumable, testable, and operator-visible.
