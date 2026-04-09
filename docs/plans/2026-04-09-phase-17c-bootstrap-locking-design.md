# Phase 17c Bootstrap Locking Design

## Objective

Prevent concurrent first-start bootstrap and migration races for a shared `ODIN_ROOT`.

## Scope

This change is intentionally narrow:

- guard first-time bootstrap and migrations with an `ODIN_ROOT`-scoped lock
- keep all entrypoints on the same `bootstrap.Load` path
- make migrations safe to re-check after waiting on the lock
- support deterministic wait-or-timeout behavior for non-owner processes

This phase does not change runtime feature behavior beyond bootstrap coordination.

## Design

### Lock scope

`bootstrap.Load` owns the bootstrap critical section. It should:

1. create the minimal runtime directories needed for the lock path
2. acquire an exclusive lock at `state/cache/bootstrap.lock`
3. open the SQLite store
4. run migrations
5. load registry and config-backed runtime state
6. seed readiness rows
7. release the lock

That keeps first-start initialization serialized per runtime root.

### Wait and timeout behavior

Non-owner processes should wait for the owner to finish bootstrap. Waiting is bounded by:

- an explicit context deadline, when present
- otherwise a configurable bootstrap timeout

If the timeout expires, the caller should receive a clear bootstrap-in-progress timeout error.

### Migration safety

After acquiring the lock, the process must query `schema_migrations` and apply only still-pending migrations. Migration application should also re-check the target version inside the transaction before executing SQL, so a repeated call after waiting is idempotent.

### Configuration

Bootstrap timeout is configured with `ODIN_BOOTSTRAP_TIMEOUT` using Go duration syntax such as `15s` or `2m`. If unset, bootstrap waits indefinitely unless the caller context has a deadline.

### Tests

The focused tests for this phase should prove:

- concurrent `bootstrap.Load` calls succeed on the same fresh runtime root
- only one caller owns the bootstrap critical section at a time
- a waiting caller receives a clear timeout error when configured
- sequential startup remains unchanged

