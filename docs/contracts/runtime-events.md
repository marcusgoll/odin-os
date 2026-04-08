---
title: Runtime Event Contract
status: active
date: 2026-04-08
phase: "03"
---

# Runtime Event Contract

This document defines the baseline runtime event envelope for Odin OS. SQLite is the canonical runtime authority, and the `events` table is the append-only audit stream for important runtime mutations.

## Event envelope

Every stored event must include:

- `stream_type`
- `stream_id`
- `event_type`
- `event_version`
- `scope`
- optional `project_id`
- optional `task_id`
- optional `run_id`
- `payload_json`
- `occurred_at`

## Stream types

Phase 03 stream types are:

- `project`
- `task`
- `run`
- `approval`
- `incident`
- `recovery`
- `registry_version`
- `executor_health`
- `context_packet`

## Event types

Phase 03 event types are:

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

## Contract rules

- Every important runtime mutation must append an event in the same SQL transaction as the row change.
- Event payloads are JSON, but event type names and envelope fields are stable typed contracts.
- Events are append-only. Corrections happen through later events, not in-place event mutation.
- Operator projections are derived and read-only.
- Event history must be sufficient to replay basic lifecycle state for tasks, runs, and approvals.

## Replay expectation

Phase 03 replay support must be able to reconstruct:

- task status state
- run status state
- approval state

This replay is a correctness requirement for lifecycle auditing and restart safety.
