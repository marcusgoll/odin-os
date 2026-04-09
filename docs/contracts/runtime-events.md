---
title: Runtime Event Contract
status: active
date: 2026-04-09
phase: "13"
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

Phase 03 through Phase 08 stream types are:

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

Phase 03 through Phase 13 event types are:

- `project.created`
- `task.created`
- `task.status_changed`
- `run.started`
- `run.finished`
- `approval.requested`
- `approval.resolved`
- `incident.opened`
- `incident.resolved`
- `incident.escalated`
- `recovery.started`
- `recovery.action_executed`
- `recovery.completed`
- `registry_version.recorded`
- `executor_health.recorded`
- `context_packet.created`
- `project.transition_changed`
- `project.shadow_observation_recorded`
- `project.compare_report_recorded`
- `project.transition_denied`

## Contract rules

- Every important runtime mutation must append an event in the same SQL transaction as the row change.
- Event payloads are JSON, but event type names and envelope fields are stable typed contracts.
- Events are append-only. Corrections happen through later events, not in-place event mutation.
- Operator projections are derived and read-only.
- Event history must be sufficient to replay basic lifecycle state for tasks, runs, and approvals.

## Context packet payload

`context_packet.created` now records the packet envelope needed for durable wake-packet handoffs:

- `packet_kind`
- `packet_scope`
- `trigger`
- `status`
- `summary`

## Replay expectation

Phase 03 replay support must be able to reconstruct:

- task status state
- run status state
- approval state

This replay is a correctness requirement for lifecycle auditing and restart safety.

## Transition expectation

Phase 13 extends the runtime event stream so project onboarding and cutover remain auditable:

- every transition state change must append `project.transition_changed`
- shadow and compare records must append explicit project report events
- denied transition-gated mutations must append `project.transition_denied`

## Self-heal expectation

Phase 11 extends the runtime event stream so deterministic self-heal actions are auditable:

- incident status changes caused by self-heal must append explicit incident events
- every bounded recovery action attempt must append `recovery.action_executed`
- escalation must appear in both recovery state and incident state, not only in logs
