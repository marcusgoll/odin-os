---
title: Real-World Readiness Contract
status: active
date: 2026-04-16
phase: "17"
---

# Real-World Readiness Contract

Odin is only allowed to claim real-world readiness when it has one real executor lane, not just a stub or a deferred adapter surface.

## Real executor lane

A real executor lane is a configured, live lane that can accept a task, execute it outside the test harness, and return an auditable result.

Rules:

- The lane must be backed by a configured driver or equivalent live provider path.
- The lane must produce real task output, not a placeholder marker, canned completion string, or deferred stub.
- The lane must be observable through the runtime so health and readiness checks can distinguish it from contract-only wiring.

## Fresh runtime readiness

- A fresh runtime without a configured driver is not ready.
- A fresh runtime with a configured driver can be ready only if the driver-backed lane is actually healthy and routable.
- Healthcheck output must reflect the runtime state honestly; it must not advertise readiness when the only executor path is a placeholder.

## Worktree cleanup invariants

- Mutable work must use a task-owned worktree and branch when it is allowed to mutate.
- Task-owned worktrees are temporary runtime artifacts and must be released or cleaned up when the task ends.
- Cleanup must not be skipped because the task completed successfully; success still requires lease and worktree release.
- Failed or interrupted tasks must leave behind enough audit trail to explain what happened, but not leak live mutable worktrees.

## Allowed alpha claims

- Bound local alpha dogfooding on a runtime that has one live driver-backed lane.
- CLI, healthcheck, backup, restore, and transition workflows that are exercised against the real runtime root.
- Explicitly bounded project work in the configured alpha envelope.

## Explicit non-goals

- No claim that a placeholder executor surface is live just because the adapter exists.
- No claim that deferred provider-backed lanes are ready before they are configured and observed healthy.
- No general unattended multi-provider autonomy claim.
- No production-readiness claim from contract-only wiring.
