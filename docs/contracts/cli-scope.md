---
title: CLI Scope Contract
status: active
date: 2026-04-08
phase: "04"
---

# CLI Scope Contract

Odin must always know and show its current scope. Scope is explicit operator state, not a guess from the current working directory.

## Supported scopes

- `global`
- `odin-core`
- `project`
- `new-project`

## Resolution order

1. An explicit command target wins.
2. A selected `system_project` resolves to `odin-core`.
3. A selected non-system project resolves to `project`.
4. New-project flows resolve to `new-project` when no explicit project target is set.
5. Otherwise Odin remains in `global`.

## Notes

- CWD-based hints may improve operator messages but do not define authoritative scope.
- `odin-core` is a reserved system-project scope and must be surfaced distinctly from normal project scope.
- Later CLI and TUI work should read this contract rather than invent scope rules ad hoc.
