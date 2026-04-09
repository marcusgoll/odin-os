---
title: Phase 10 Observability Design
status: accepted
date: 2026-04-09
phase: "10"
---

# Phase 10 Observability Design

## Goal

Introduce first-class observability for Odin OS so operators can tell what is healthy, what is degraded, what has gone stale, which items are blocked, and what recoveries Odin has already attempted.

## Chosen Approach

Phase 10 will add:

- structured logs with correlation identifiers
- typed health checks and a shared doctor report
- metrics snapshots and export
- read-only operator projections for incidents, recoveries, freshness, approvals, blocked items, and portfolio state
- source and projection freshness tracking backed by SQLite

This keeps observability machine-parseable and aligned with Odin's existing runtime authority rather than creating a second monitoring system.

## Rejected Alternatives

### CLI-only doctor output

Rejected because it would mostly reformat SQL results and would not create reusable machine-readable health or metrics surfaces for later API and dashboard work.

### Log-first observability

Rejected because raw string logs alone are not enough for health checks, operator projections, or structured diagnosis. Logs should complement runtime projections, not replace them.

### Separate monitoring database

Rejected because SQLite is already the canonical runtime authority. Observability should derive from canonical runtime state and auditable events, not replicate them elsewhere.

## Structured Logging

Phase 10 should add a structured logger under `internal/telemetry/logs`.

Each log record should include:

- timestamp
- level
- component
- message
- correlation identifier
- scope
- optional project, task, run, and approval identifiers
- arbitrary structured fields

Logs should be renderable as JSON lines and should support emitting to `runs/logs/` without becoming the canonical audit source.

## Health Model

The current health summary is too thin for operator use. Phase 10 should replace it with a typed doctor report containing component checks for:

- database connectivity
- registry load health
- executor availability and freshness
- queue pressure
- stale projections
- source freshness

Each check should produce a structured result with:

- check name
- status
- summary
- details
- observed timestamp

The overall doctor status should derive from those checks rather than hardcoded string rules.

## Source Freshness

Source freshness should compare current runtime data to its last known refresh point.

Phase 10 should track freshness for at least:

- registry compilation
- executor health sampling
- operator projection refresh

The primary runtime freshness authority should remain in SQLite, not in-memory timestamps.

## Projection Freshness

Phase 10 should introduce a small freshness ledger for projections, stored in SQLite, so the system can answer whether a projection surface is fresh, degraded, or stale.

This ledger should cover at least:

- doctor
- metrics
- operator projections

## Metrics Model

Metrics should be derived from canonical runtime state and exposed through a shared metrics snapshot plus export formatter.

Phase 10 should include metrics for:

- active runs
- queued tasks
- blocked items
- approvals waiting
- open incidents
- running recoveries
- stale executors
- stale sources
- stale projections

The initial export format should be plain text and machine-readable so future HTTP export can reuse it.

## Operator Projections

Phase 10 should extend read-only operator projections to include:

- active runs
- blocked items
- approvals waiting
- incidents
- recoveries
- freshness
- project portfolio view

Blocked items should be derived from pending approvals, open incidents, and active wake packets with blocking reasons.

The project portfolio view should summarize project health and work state rather than just counting tasks.

## Doctor Surface

The `/doctor` command should move from a thin one-line summary to a shared doctor service that supports:

- human-readable text output
- machine-readable JSON output

The CLI should default to compact text and accept `/doctor json` for structured output.

## Incidents and Recoveries

Phase 10 does not redesign incident or recovery persistence. Instead it should add read helpers, summaries, and projections so operators can see:

- current open incidents
- recovery attempts in progress
- latest recovery outcomes
- what still requires human attention

## Package Boundaries

Phase 10 should use:

- `internal/telemetry/logs` for structured logging
- `internal/telemetry/metrics` for metrics snapshots and export
- `internal/telemetry/audit` only for derived audit-facing helpers when needed
- `internal/runtime/health` for doctor and health checks
- `internal/runtime/projections` for operator-facing read-only views
- `internal/cli` for doctor rendering only

Observability must consume runtime state; it must not become a second runtime authority.

## SQLite Authority

Phase 10 should add the minimum schema needed for projection freshness tracking rather than storing health state only in memory.

This likely means a new `projection_freshness` table keyed by projection surface with:

- surface name
- status
- refreshed at
- details

## Testing Strategy

Tests should prove:

- doctor distinguishes healthy, degraded, and failed states
- metrics reflect real runtime conditions
- incident and recovery projections are visible
- source freshness becomes degraded when expected
- projection freshness becomes stale when expected
- `/doctor json` is machine-parseable

## Phase Boundary

Phase 10 introduces:

- structured logs
- typed health and doctor surfaces
- metrics export
- operator projections for blocked work and recoveries
- freshness tracking

Phase 10 does not yet introduce:

- full tracing instrumentation
- remote telemetry sinks
- background schedulers dedicated solely to observability
- a web dashboard implementation
