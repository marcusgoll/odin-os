---
title: Operational Autonomy
status: proposed
date: 2026-04-11
---

# Operational Autonomy

This document defines when `odin-os` may claim primary operational control for unattended work.

## Primary controller

`odin-os` is the primary controller for a project only when it is the default system responsible for:

- accepting normal project work
- selecting and starting runs
- enforcing project policy and transition state
- recording approvals, checkpoints, and recovery state
- surfacing operator-visible status for that work

Legacy systems may remain available as rollback paths during cutover, but they are not the primary controller once `odin-os` owns the normal task lifecycle.

## Approval-required action classes

Human approval remains mandatory for:

- system-project mutation
- destructive git or filesystem operations
- governance or policy changes
- high-risk bounded actions
- any mutation outside the active project transition allowlist

Normal unattended work may proceed without approval only when project policy and transition state both allow it.

## Required runtime invariants

`odin-os` may not claim operational autonomy unless all of the following are true:

1. Fresh bootstrap is healthy without manual seeding.
2. At least one executor lane completes real durable work end to end.
3. High-risk work is blocked behind explicit approval records.
4. Every mutable run uses a leased task-owned branch and worktree.
5. Restart and recovery state are persisted and used after interruption.
6. Operator-visible status explains queued, running, blocked, failed, and stalled work.
7. Multi-project queue control enforces concurrency, budget, and starvation limits.

## Cutover gates

Operational cutover must happen in explicit gates:

1. truthful alpha
2. single-project live autonomy
3. multi-project shadow control
4. multi-project primary control
5. legacy retirement

Each gate requires runtime evidence, not package-level test coverage alone.

## Rollback triggers

Cutover must pause or roll back when any of the following occur:

- successful completions stop being recent
- stalled or dead-letter work grows without bounded recovery
- approval flows stop protecting high-risk mutations
- operator surfaces cannot explain current control ownership
- a pilot project requires the legacy stack for routine completion
