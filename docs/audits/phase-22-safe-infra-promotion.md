# Phase 22 Safe Infrastructure Promotion

## Summary

Phase 22 promotes only the inert limited-action infrastructure from the experimental line onto a `main`-based branch.

Promoted:

- persisted `task.action_key`
- manifest parsing and validation for bounded-action declarations
- fail-closed runtime handling for explicit bounded-action tasks
- lease inspection and cleanup shell surfaces

Explicitly not promoted:

- bounded mutation executor paths
- experimental `pbs` limited-action policy changes
- any configuration that moves `pbs` off `shadow`

## What was promoted

### Runtime and store

- SQLite migration `0007_task_action_keys.sql`
- `Task.ActionKey` and `CreateTaskParams.ActionKey`
- `task.created` runtime event payload now includes `action_key`

### Project policy model

- `policy.limited_actions`
- known bounded-action key catalog
- validation for:
  - `description`
  - `path_prefixes`
  - `target_path`
  - `content_mode`

### Operator lease surfaces

- `/leases`
- `/leases active`
- `/leases released`
- `/leases all`
- `/leases inspect <lease-id>`
- `/leases cleanup confirm`

## What remained intentionally deferred

- `internal/executors/codex/adapter.go` bounded mutation paths
- `docs_audit_note` execution success paths
- `docs_update` execution success paths
- `repo_hygiene_note` execution support
- experimental `pbs` manifest declarations for bounded actions
- pilot docs that describe real bounded mutation success on `pbs`

## Runtime behavior on this promotion branch

The promotion branch understands explicit bounded-action tasks, but it does not execute them.

Expected behavior:

- `action:<key> ...` is parsed and persisted
- if the project does not declare the key, runtime fails with:
  - `action key "<key>" is not supported by project policy`
- if the project does declare the key, runtime still fails with:
  - `action key "<key>" is not enabled on this line`
- failure occurs before lease allocation or executor mutation

## Verification

Focused verification passed:

- `go test ./internal/runtime/jobs ./internal/core/projects ./internal/cli/repl ./internal/store/sqlite ./internal/vcs/... -count=1`
- `make build`

The new tests cover:

- explicit action-key parsing and persistence
- bounded-action manifest rule parsing
- fail-closed bounded-action execution on this line
- lease inspection
- lease cleanup

## Promotion recommendation

This subset is safe to promote later because it strengthens observability and policy structure without broadening mutation authority.

Blunt recommendation:

- safe to consider for operational promotion
- not sufficient to move `pbs` out of `shadow`
- do not promote any bounded mutation executor behavior with it

## Operational rule

`pbs` remains `shadow`-only on the operational line.
