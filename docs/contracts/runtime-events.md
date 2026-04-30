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
- `incident`
- `recovery`
- `registry_version`
- `executor_health`
- `context_packet`
- `learning_proposal`
- `learning_evaluation`
- `learning_promotion`
- `skill`

## Event types

Phase 03 through Phase 14 event types are:

- `project.created`
- `task.created`
- `task.status_changed`
- `task.queue_state_changed`
- `run.started`
- `run.status_changed`
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
- `learning.proposal_created`
- `learning.proposal_submitted`
- `learning.proposal_rejected`
- `learning.evaluation_recorded`
- `learning.promotion_applied`
- `learning.promotion_rolled_back`
- `skill.lifecycle_recorded`

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
- task queue state
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

## Failure analysis expectation

Failed runs remain visible through `run.finished` events. When Odin can classify
a failed Codex run, refactor, review, or migration step, the run artifacts may
include a `failure_analysis` object following `docs/contracts/failure-analysis.md`.

Failure analysis is advisory. It may recommend a follow-up issue, but it must
not auto-apply prompt, skill, workflow, architecture, shim, or implementation
changes.

## Skill lifecycle expectation

Skill CRUD and invocation now append `skill.lifecycle_recorded` events when they run through Odin's runtime app path.

CRUD lifecycle events keep `scope=repo`. Invoke lifecycle events record the normalized runtime scope so project- and odin-core-scoped activity remains filterable alongside other runtime events.

The payload must include enough information to reconstruct operator-visible skill activity:

- `skill_key`
- `operation`
- `outcome`
- optional `execution_profile`
- `version`
- `handler_type`
- `handler_ref`
- `permissions`
- `duration_ms`
- optional `error_code`
- optional `error_text`

Permission-gated invoke denials use stable `error_code` values so operators and tests can distinguish policy failures from generic handler failures:

- `unknown_permission`
- `mutation_requires_project_scope`
- `transition_denied`
- `approval_required`

Because skill files live in the repo rather than SQLite, these events are appended immediately after the lifecycle action instead of sharing a SQL transaction with a row mutation. The repo remains the source of truth; the event stream is the auditable runtime trail.

Allowed invokes record `execution_profile=restricted_command_v1`. Denied pre-exec invokes leave `execution_profile` empty so the event stream does not claim a wrapper profile that never ran.
