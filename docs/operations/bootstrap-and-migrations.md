# Bootstrap And Migrations

## Purpose

Odin serializes first-start bootstrap work per `ODIN_ROOT` so multiple processes can safely target the same fresh runtime root.

## Locking behavior

Bootstrap uses an exclusive lock at:

- `state/cache/bootstrap.lock`

The lock is scoped to the runtime root. While one process holds it, other processes targeting the same `ODIN_ROOT` wait for bootstrap to finish before opening the shared runtime.

The lock covers:

1. SQLite open and migrations
2. registry load
3. project manifest load
4. executor config load
5. readiness-state seeding

## Timeout behavior

By default, waiters block until bootstrap completes or the caller context is canceled.

Set `ODIN_BOOTSTRAP_TIMEOUT` to bound the wait using Go duration syntax:

- `ODIN_BOOTSTRAP_TIMEOUT=15s`
- `ODIN_BOOTSTRAP_TIMEOUT=2m`

If the timeout expires, Odin returns a bootstrap-in-progress timeout error instead of racing migrations.

## Migration behavior

After acquiring the bootstrap lock, Odin re-reads `schema_migrations` and applies only pending versions. Each migration also re-checks its target version inside the transaction before executing SQL.

Sequential startup behavior is unchanged.

## Operator guidance

- Point `doctor`, `healthcheck`, the interactive shell, and `serve` at the same `ODIN_ROOT` freely.
- Use `ODIN_BOOTSTRAP_TIMEOUT` when you want callers to fail fast instead of waiting for a long first-start bootstrap.
- Investigate a stuck lock holder by checking the active Odin process using that runtime root rather than deleting the lock file blindly.
