---
title: Odin OS Runtime State Migration Plan
status: active
date: 2026-04-30
---

# Odin OS Runtime State Migration Plan

## Current Persistence Inventory

SQLite is the current runtime authority. The active implementation is `internal/store/sqlite` and the active database is built by migrations under `internal/store/sqlite/migrations/`.

| Target state area | Current storage | Current package | Decision |
| --- | --- | --- | --- |
| `issues` | External GitHub issue intake persists in `external_issues`; reconciliation materializes eligible rows into `tasks` plus `task_intakes` evidence. | `internal/tracker`, `internal/tracker/intake`, `internal/runtime/jobs`, `internal/store/sqlite` | Keep GitHub read-only; reconcile from SQLite into Work Items before any separate dispatch step. |
| `workspaces` | `worktree_leases` table. | `internal/vcs/leases`, `internal/vcs/worktrees`, `internal/store/sqlite` | Wrap existing lease state. |
| `agent_runs` | `runs` table. | `internal/runtime/jobs`, `internal/runtime/runs`, `internal/store/sqlite` | Wrap existing runs as Agent Runs. |
| `run_events` | `events` table. | `internal/runtime/events`, `internal/store/sqlite` | Wrap existing append-only events. |
| `pull_requests` | PR handoff and read-only review result state persists in `pull_request_handoffs` and `pull_request_review_results`. | `internal/review`, `internal/store/sqlite`, `.github/*` | Keep SQLite as restart-safe runtime authority; live GitHub mutation remains adapter/proof-gated. |
| `locks` | No general lock table. Bootstrap has filesystem lock behavior; SQLite has uniqueness constraints. | `internal/app/bootstrap`, SQLite indexes | Add only if a runtime lock model is required. |
| `failures` | `incidents` and `recoveries` tables. | `internal/runtime/recovery`, `internal/store/sqlite` | Wrap incidents first; keep recovery state separate. |

Other current state areas remain in place: projects, tasks, approvals, context packets, projection freshness, project transitions, learning, actions/evidence, knowledge sources, and memory summaries.

## Decision

Use a wrap-first migration, adding schema only when existing runtime tables do
not contain the required state.

The first step added typed models and repository interfaces in `internal/db`,
backed by the existing `internal/store/sqlite.Store`. Read-only GitHub intake
adds `external_issues` as the smallest explicit schema extension because no
existing table stores external issue identity, labels, body hash, sync status,
and stable sync cursor idempotently. PR handoff persistence adds
`pull_request_handoffs` and `pull_request_review_results` because no existing
table stores PR URL/number, linked issue, branch, selected review roles,
evidence lists, blockers, comments, and read-only review outcomes
restart-safely.

Rationale:

- Existing restart recovery depends on current `runs`, `events`, `incidents`, `recoveries`, and worktree lease behavior.
- Current tests already cover store migration, worktree leases, recovery, context packets, actions, projections, knowledge, and memory summaries.
- Renaming tables now would create unnecessary migration and rollback risk.
- Target vocabulary can be projected over existing tables where state already
  exists; external issue intake uses a dedicated table because GitHub must not
  become runtime authority.

## Implemented Wrapper

`internal/db.NewSQLiteRepository(store)` currently maps:

- `external_issues` -> `Issue`
- `pull_request_handoffs` -> `PullRequest`
- `runs` -> `AgentRun`
- `events` -> `RunEvent`
- `worktree_leases` -> `Workspace`
- `incidents` -> `Failure`

Missing target repositories return explicit `ErrRepositoryNotMigrated`:

- `locks`

This prevents silent success against nonexistent state.

## Backup Instructions

Before any future schema migration against a live runtime root:

1. Stop or pause worker execution where practical.
2. Identify the runtime DB path from `ODIN_ROOT` or the systemd env file.
3. Create a dated backup directory under the runtime root.
4. Use SQLite backup semantics, not raw partial copies during writes.

Example:

```bash
runtime_root="${ODIN_ROOT:-$HOME/.local/state/odin-os}"
backup_dir="$runtime_root/backups/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$backup_dir"
sqlite3 "$runtime_root/odin.db" ".backup '$backup_dir/odin.db'"
```

If `sqlite3` is unavailable, use the repo-owned backup command:

```bash
ODIN_ROOT="$runtime_root" ./bin/odin backup "$backup_dir/odin-backup.tar.gz"
ODIN_ROOT="$runtime_root" ./bin/odin verify-backup "$backup_dir/odin-backup.tar.gz"
```

## Rollback Path

For wrapper-only slices, rollback is code-only:

1. Revert the `internal/db` wrapper changes and docs.
2. Keep the SQLite database untouched.
3. Run `go test ./...`.
4. Run `ODIN_ROOT=<tmp> ./bin/odin doctor --json`.

For additive schema migrations such as PR handoff persistence:

1. Stop the service.
2. Restore the backed-up DB to the runtime root.
3. Start the service.
4. Verify `./bin/odin healthcheck` or `/readyz`.
5. Inspect startup recovery output before resuming worker dispatch.

## Migration Risks

| Risk | Mitigation |
| --- | --- |
| Work Item / Agent Run vocabulary drifts from `tasks` / `runs`. | Keep typed wrappers explicit until a schema ADR exists. |
| Missing PRs or locks look implemented because interfaces exist. | Return `ErrRepositoryNotMigrated` until tables and migrations exist. |
| Restart recovery breaks because run/incident state changes shape. | Do not alter `runs`, `events`, `incidents`, `recoveries`, or recovery queries in wrapper slices. |
| Live DB is changed without backup. | Require the backup procedure above before any schema migration. |
| Direct SQL assumes stale column names. | Inspect schema or use repository methods before writing queries. |

## Next Migration Tickets

1. Add pull request handoff tables only after PR manager behavior is specified.
2. Decide whether locks are SQLite rows, projection freshness records, or filesystem locks before adding a table.
3. Add repository methods for active agent runs and cleanup-eligible workspaces.
4. Move services one at a time from direct `sqlite.Store` calls to `internal/db` interfaces after characterization tests exist.
