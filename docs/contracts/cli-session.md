---
title: CLI Session Contract
status: active
date: 2026-04-09
phase: "05"
---

# CLI Session Contract

Odin persists light operator context for both the compatibility REPL and explicit root commands.

Session state is stored in `state/cache/cli-session.json`.
Reusable session primitives live in `internal/cli/state`; the REPL package re-exports them for compatibility, and root commands reuse the same state loader and saver.

## Persisted fields

- `project_key`
- `mode`

## Restore rules

When a command or the REPL starts, Odin must:

1. read the saved session cache
2. validate `project_key` against the current project manifest registry
3. resolve scope from the validated project
4. validate `mode` against the resolved scope
5. restore the saved values only when valid
6. otherwise downgrade safely to `ask` and, when needed, `global`

## Safety rules

- invalid or missing saved projects fall back to `global`
- unsafe saved modes fall back to `ask`
- no active task, run, or prompt history is persisted
- `global + act` is invalid
- explicit commands may ignore persisted mode when the command requires a different execution path

## Notes

- `odin-core` persists through `project_key: odin-core`
- `new-project` is not persisted independently in Phase 05
- persisted state is advisory and must be revalidated on every command invocation
