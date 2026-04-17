---
title: Workspace Bootstrap
status: active
updated: 2026-04-17
---

# Workspace Bootstrap

Odin OS now treats the workspace model as bootstrap state, not optional seed data.

## Fresh runtimes

Any command path that goes through `internal/app/bootstrap.Load` will now ensure:

- the default workspace exists
- the default companion exists for that workspace
- existing managed projects are reconciled into project-backed initiatives
- legacy tasks are linked into the workspace model without changing the physical `tasks` table name

This means a fresh runtime can start from an empty `data/odin.db` and still expose workspace, initiative, companion, and work-item semantics immediately.

## Repairing existing runtimes

For older runtimes that already contain `projects` and `tasks`, run:

```bash
go run ./scripts/migrate/bootstrap_workspace -runtime-root /path/to/odin-root
```

If `ODIN_ROOT` is already set, the flag is optional:

```bash
ODIN_ROOT=/path/to/odin-root go run ./scripts/migrate/bootstrap_workspace
```

The helper will:

- bootstrap the default workspace and default companion
- reconcile initiatives for existing managed projects
- bind legacy tasks to the default workspace
- link legacy tasks to matching managed-project initiatives
- backfill empty `work_kind` values from the existing task scope
- assign a companion to repaired tasks only when the linked initiative already has an owner companion

## Safety notes

- The helper is additive and idempotent.
- It does not rename legacy tables.
- It does not rewrite project identities.
- It does not overwrite an existing initiative owner companion.
- It preserves compatibility with existing runtime reads while improving the semantic links needed by new operator surfaces.
