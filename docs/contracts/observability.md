# Observability Contract

## Purpose

Phase 10 defines the structured observability surfaces Odin uses for logs, health, metrics, and operator-facing projections.

## Logging

Structured logs are JSON lines with:

- timestamp
- level
- component
- message
- correlation identifier
- scope
- optional project, task, and run identifiers
- structured fields

## Doctor

Doctor reports are structured and machine-parseable. They must distinguish:

- `healthy`
- `degraded`
- `failed`

## Metrics

Metrics are derived from canonical runtime state and exported in machine-readable text form.

Required metrics include:

- active runs
- blocked items
- approvals waiting
- open incidents
- active recoveries
- queued tasks
- stale executors
- stale sources
- stale projections

## Projections

Operator projections remain read-only and must not mutate runtime state.
