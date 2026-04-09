# Phase 17c Bootstrap Locking Audit

## Issue

Parallel first-start use of the same fresh `ODIN_ROOT` could race on SQLite migrations and fail with:

- `UNIQUE constraint failed: schema_migrations.version`

This was observed when `doctor`, `healthcheck`, and shell startup bootstrapped the same fresh runtime root concurrently.

## Resolution

The shared `bootstrap.Load` path now acquires an exclusive runtime-root lock before opening SQLite or applying migrations. Waiting callers block until bootstrap completes, or return a clear timeout error when `ODIN_BOOTSTRAP_TIMEOUT` is exceeded.

The migration path was also tightened so each migration re-checks whether its version is already applied inside the transaction before executing SQL.

## Evidence

Focused tests now cover:

- concurrent `bootstrap.Load` on the same fresh runtime root
- waiting behavior for non-owner callers
- timeout behavior for bounded waiters

These checks live in [bootstrap_test.go](/home/orchestrator/.config/superpowers/worktrees/odin-os/phase-17-alpha-stabilization/internal/app/bootstrap/bootstrap_test.go).

## Remaining scope

This phase does not add cross-host distributed coordination. The guarantee is per shared filesystem runtime root, which is the intended homelab operating model for Odin alpha.
