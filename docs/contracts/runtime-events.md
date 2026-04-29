---
title: Runtime Event Contract
status: active
date: 2026-04-09
phase: "14"
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

Phase 03 through Phase 14 stream types are:

- `project`
- `task`
- `run`
- `approval`
- `action`
- `incident`
- `recovery`
- `registry_version`
- `executor_health`
- `context_packet`
- `learning_proposal`
- `learning_evaluation`
- `learning_promotion`

## Event types

Phase 03 through Phase 14 event types are:

- `project.created`
- `task.created`
- `task.status_changed`
- `run.started`
- `run.finished`
- `approval.requested`
- `approval.resolved`
- `action.prepared`
- `action.preflighted`
- `action.approved`
- `action.submitted`
- `action.internally_recorded`
- `action.externally_read_back`
- `action.completed`
- `action.failed`
- `action.abandoned`
- `action.corrected`
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
- `learning.proposal_created`
- `learning.proposal_submitted`
- `learning.proposal_rejected`
- `learning.evaluation_recorded`
- `learning.promotion_applied`
- `learning.promotion_rolled_back`

## Contract rules

- Every important runtime mutation must append an event in the same SQL transaction as the row change.
- Event payloads are JSON, but event type names and envelope fields are stable typed contracts.
- Events are append-only. Corrections happen through later events, not in-place event mutation.
- Operator projections are derived and read-only.
- Event history must be sufficient to replay basic lifecycle state for tasks, runs, and approvals.

## Action evidence expectation

Action evidence extends the runtime event stream with an `action` stream. Every row appended to `action_evidence_events` must mirror a generic runtime event in the same SQL transaction.

The mirrored event envelope must use:

- `stream_type`: `action`
- `stream_id`: the `actions.id` value
- `event_type`: the stable action evidence event type, such as `action.prepared`, `action.submitted`, or `action.externally_read_back`
- `event_version`: the action evidence event version
- `run_id`: the `run_id` supplied to the action evidence append, when present

The mirrored payload must include linkage fields:

- `evidence_id`
- `action_id`
- `payload_hash`
- `approval_id`, when present
- `run_id`, when present
- `source`

The generic runtime event is an audit mirror of the action evidence row. The `action_evidence_events` table remains the action-specific evidence source, and corrections must be represented by later action events such as `action.corrected`, not by mutating prior evidence or event rows.

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

## Self-improvement expectation

Phase 14 extends the runtime event stream so bounded self-improvement remains auditable:

- every proposal creation and submission must append explicit learning proposal events
- every deterministic evaluation must append `learning.evaluation_recorded`
- every runtime activation must append `learning.promotion_applied`
- every rollback must append `learning.promotion_rolled_back`

## Self-heal expectation

Phase 11 extends the runtime event stream so deterministic self-heal actions are auditable:

- incident status changes caused by self-heal must append explicit incident events
- every bounded recovery action attempt must append `recovery.action_executed`
- escalation must appear in both recovery state and incident state, not only in logs
