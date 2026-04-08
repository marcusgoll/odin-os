# Phase 03 SQLite Store, Events, and Projections Design

**Date:** 2026-04-08

## Goal

Establish SQLite as Odin's real runtime authority with auditable event append behavior, replayable lifecycle state, and basic read-only projections for operator and recovery workflows.

## Recommended Approach

Use a hybrid transactional design:

- canonical current-state tables for the runtime entities Odin must query directly
- an append-only `events` table for every important mutation
- projection helpers that derive operator views from current rows and event history without introducing separate authoritative projection tables yet

This matches the existing authority ADR without forcing a full event-sourced runtime before the task and run model is stable.

## Schema

Phase 03 will add the following tables:

- `schema_migrations`
- `projects`
- `tasks`
- `runs`
- `approvals`
- `events`
- `incidents`
- `recoveries`
- `registry_versions`
- `executor_health`
- `context_packets`

### Entity responsibilities

- `projects`: managed-project and `odin-core` enrollment/runtime rows
- `tasks`: high-level unit of runtime work
- `runs`: executor attempts and execution lifecycle
- `approvals`: bounded operator approval lifecycle
- `incidents`: auditable runtime failures or safety events
- `recoveries`: deterministic recovery attempts tied to incidents or runs
- `registry_versions`: compiled registry snapshots and version hashes
- `executor_health`: executor health checks and readiness snapshots
- `context_packets`: durable wake-packet and compaction handoff payloads

### Event table

The `events` table stores:

- stream metadata
- event type and version
- optional project, task, and run foreign keys
- explicit scope
- JSON payload
- occurrence timestamp

Every important mutation writes its state change and event row inside one SQL transaction.

## Event Contract

Phase 03 defines a small typed event envelope in Go plus a human-readable contract doc. Initial event types:

- `project.created`
- `task.created`
- `task.status_changed`
- `run.started`
- `run.finished`
- `approval.requested`
- `approval.resolved`
- `incident.opened`
- `recovery.started`
- `recovery.completed`
- `registry_version.recorded`
- `executor_health.recorded`
- `context_packet.created`

Payloads remain JSON, but event type names and envelope fields are strongly defined.

## Store Layer

The store will live in `internal/store/sqlite` and provide:

- open/close helpers
- migration runner
- lifecycle mutation methods for the core tables
- transactional event append helper
- event listing helpers

The store remains intentionally small in Phase 03. It is not a generic repository abstraction or distributed event bus.

## Projections

Phase 03 introduces two projection styles:

1. SQL-backed current-state helpers
   - task status summary
   - run summary
   - pending approvals
   - project transition view

2. Event replay helpers
   - reconstruct task and run state from ordered events
   - reconstruct approval state from ordered events

This gives Odin direct operator reads now while proving that event history is already replayable.

## Migration Strategy

Use ordered SQL migrations embedded in the binary. The migrator:

- creates the schema on an empty DB
- records applied versions in `schema_migrations`
- skips previously applied versions cleanly

Phase 03 will start with one baseline migration that creates the runtime schema.

## Testing Strategy

Add tests for:

- migration on an empty database
- migration re-run on an existing database
- lifecycle operations for projects, tasks, runs, and approvals
- auditable event append during every important mutation
- event replay into basic projections
- persistence after reopening the same database file

## Non-Goals

- distributed storage
- background projection daemons
- materialized projection tables
- registry persistence in SQLite
- full incident automation or recovery orchestration
