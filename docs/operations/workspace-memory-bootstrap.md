# Workspace Memory Bootstrap

## Purpose

Odin OS now bootstraps canonical workspace-era memory ownership during app load. Fresh runtimes create the default Marcus workspace, and existing managed project memory is backfilled into workspace- and initiative-owned records without rewriting runtime lineage.

## What bootstrap does

During `internal/app/bootstrap.Load` and `LoadReadOnly`, Odin now:

1. ensures the default `marcus` workspace exists
2. reconciles existing non-system runtime projects into `managed_project` initiatives linked to their `projects` rows
3. backfills legacy memory ownership in place

The backfill is idempotent and preserves existing lineage fields:

- `project_id`
- `task_id`
- `run_id`
- `source_transcript_id`

## Ownership migration rules

- legacy `global` transcripts and memory summaries become workspace-owned records
- global memory scope is rewritten from `global` to `workspace` with scope key `marcus`
- existing project transcripts gain `workspace_id` and `initiative_id`
- existing project memory summaries gain `workspace_id`, `initiative_id`, `visibility_scope`, and `retention_class`
- `episode` summaries default to `retention_class=episodic`
- other migrated summaries default to `retention_class=durable`

This bootstrap does not introduce orchestrator-style projections or generated hot-memory files.

## Script entrypoint

Use the migration script when you want to force the bootstrap path against a runtime root without starting a long-running service:

```bash
go run ./scripts/migrate/bootstrap_workspace.go \
  -repo-root /path/to/odin-os \
  -runtime-root /path/to/runtime
```

If `-runtime-root` is omitted, the script falls back to `ODIN_ROOT`, then to the repo root.

## Operator guidance

- Run the script once before cutover when importing an existing runtime that already contains project transcripts or memory summaries.
- It is safe to rerun; the bootstrap is idempotent and reuses existing workspace and initiative rows.
- Use the normal `odin` command after bootstrap to verify the runtime opens cleanly against the migrated database.
