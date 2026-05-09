---
title: Odin OS Shim Retirement Plan
status: draft
date: 2026-04-30
---

# Odin OS Shim Retirement Plan

## Current Normalization Slice

The first normalized shim is `internal/runner/codexexec`.

Current state:

- `internal/runner` is a compatibility seam for one role attempt in one worktree.
- `internal/executors/contract` is the canonical executor boundary.
- `internal/runner/codexexec.NewAdapter()` still preserves existing placeholder behavior and returns `runner.ErrNotImplemented`.
- `internal/runner/codexexec.NewAdapterWithExecutor(...)` routes compatibility calls through a typed `contract.Executor`.

This keeps existing callers working while giving future migration code a typed path that does not construct shell commands or bypass executor policy.

## Retirement Rules

Do not delete a shim until all of these are true:

1. A typed Go adapter exists at the canonical package boundary.
2. Existing shell or Go callers have a tested compatibility path.
3. The real `odin` command path proves the replacement behavior where applicable.
4. Security-sensitive paths have explicit review for subprocesses, filesystem mutation, GitHub tokens, secrets, and worker sandboxing.
5. The old shim has no remaining production callers, confirmed by repository search.

## `internal/runner/codexexec` Retirement Path

1. Keep `NewAdapter()` as a placeholder compatibility constructor while no real subprocess runner exists.
2. Use `NewAdapterWithExecutor(...)` only as a bridge into `internal/executors/contract`.
3. Implement real `codex exec` as an executor adapter under `internal/executors`, not as business logic inside `internal/runner`.
4. Move role-to-task-kind mapping into the canonical orchestration layer when Work Item roles are fully modeled.
5. Once no callers depend on `internal/runner`, remove it in a dedicated cleanup ticket with tests proving the executor route remains intact.

## Follow-Up Tickets

1. Add a canonical `internal/executors/codex_exec` package with command construction tests and security policy enforcement.
2. Move runner compatibility tests to executor contract tests once the real Codex exec adapter exists.
3. Replace `internal/runner/appserver` with a phase-two executor adapter only after app-server protocol ownership is decided.
4. Keep `internal/tracker/github` as the selected GitHub intake package root;
   do not move tracker behavior into `internal/adapters/github`.
5. Normalize `internal/prompts` into a typed renderer over repo-owned prompt assets.
