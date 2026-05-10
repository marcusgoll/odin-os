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
- `browser_session`

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
- `browser.session_created`
- `browser.session_status_changed`
- `browser.session_verified`
- `browser.session_revoked`
- `browser.session_profile_prepared`
- `browser.session_login_requested`
- `browser.session_login_completed`
- `browser.session_login_expired`

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

## Goal lifecycle expectation

Odin-native goals are persisted in SQLite and append runtime events for operator-visible state changes:

- `goal.created`
- `goal.updated`
- `goal.status_changed`
- `goal_runner.observed`

Goal run, blocker, and evidence records use the same stream for future runner-facing mutations:

- `goal_run.started`
- `goal_run.status_changed`
- `goal_run.finished`
- `goal.blocker_recorded`
- `goal.evidence_recorded`
- `review.approved`
- `review.rejected`

The `goal` stream is the audit trail for goal CLI mutations. Goal state remains in SQLite; registry files, overview projections, and future runners must not become a parallel goal authority.

Goal-derived review queue items use existing goal state as their authority. `intake-goal:<id>`, `goal:<id>`, and `goal-approval:<id>` can approve or reject created/planned goals through the review CLI. `goal-blocker:<id>` items are visible for inspection, but blocker resolution is not implemented until a store-level resolution primitive and lifecycle rule exist; approve/reject attempts must return an unsupported/not-resolved result without mutating goal or blocker state.

## Intake-to-goal expectation

Raw intake processing remains on the `intake_items` SQLite authority. When deterministic processing routes a raw intake item into a reviewable goal, Odin must preserve the intake-to-goal link on the intake row, leave the goal unapproved, and append audit events through the runtime event stream:

- `intake.processed`
- `intake.routed_to_goal`
- `goal.created`

The processing payload must include the source intake ID, route decision, classification result, and created goal ID when a goal is created. Intake conversion must not approve, run, or mutate external systems.

## Intake-to-proposal expectation

Raw intake processing remains on the `intake_items` SQLite authority. Processing may emit `intake.processing_started`, `intake.classified`, `intake.dedupe_reviewed`, `intake.routed`, `intake.draft_artifact_created`, `intake.clarification_needed`, `intake.duplicate_linked_or_suppressed`, and `intake.processed`.

The processing payload and routing notes must preserve enough evidence to reconstruct the Reviewable Intake Proposal. Intake processing must not create Work Items, Run Attempts, dispatches, approvals, or external mutations by default.

## Browser session handoff expectation

Manual Huginn browser login and authenticated read-only session reuse are being implemented in metadata-first slices. Browser session metadata, profile storage policy metadata, and login request metadata live in SQLite, future browser profile files must stay under `ODIN_ROOT`, and profile/request lifecycle mutations append events through the runtime event stream:

- `browser.session_created`
- `browser.session_status_changed`
- `browser.session_verified`
- `browser.session_revoked`
- `browser.session_profile_prepared`
- `browser.session_login_requested`
- `browser.session_login_completed`
- `browser.session_login_expired`
- `goal.waiting_for_human_login`

Browser session events must not include passwords, cookies, bearer tokens, passkey material, TOTP values, backup codes, profile bytes, or raw credential prompts. Login request events may include a log-safe opaque `handoff_id` and a metadata-only `handoff_url`; neither proves that a handoff HTTP route exists. Metadata-only session verification records operator-attested verification and `last_verified_at`; browser-observed account/domain verification remains future work. Profile preparation records only empty-directory preparation metadata plus `profile_storage_policy`; a prepared directory is not approval to write browser files. Session verification may unblock a waiting goal only through normal policy checks; it must not approve or execute the goal by itself.

`odin browser session handoff show --handoff-id <id>` is intentionally read-only. It validates the handoff ID, login request status, expiration, and linked session status, but emits no runtime event because it performs no state change.
