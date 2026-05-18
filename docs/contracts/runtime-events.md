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
- `memory.summary_recorded`
- `memory.summary_updated`
- `memory.proposal_created`
- `memory.proposal_resolved`
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
- `automation_trigger.created`
- `automation_trigger.fire_requested`
- `automation_trigger.evaluated`
- `automation_trigger.materialized`
- `automation_trigger.tested`
- `automation_trigger.deferred`
- `automation_trigger.errored`
- `automation_trigger.status_changed`

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

## Model routing evidence

Executor selection may record a `model_routing` run artifact alongside normal
run and executor evidence. This artifact is evidence attached to the existing
Run Attempt; it is not a separate event stream, scheduler, approval system, or
provider authority.

Stable fields may include:

- `route_name`
- `executor_lane`
- `provider_key`
- `model_key`
- `provider_model_id`
- `fallback_used`
- `policy_reason`
- `estimated_cost_usd`
- `context_window_tokens`
- `latency_tier`
- `task_class`
- `risk_class`

The artifact must not include API keys, provider credentials, token cache
values, or provider-private request payloads.

Operators can read the latest recorded routing decision without inspecting raw
artifact JSON:

```bash
odin runs routing --run <run-id> [--json]
odin runs routing --task <task-id-or-key> [--json]
```

This command reads existing Task, Run Attempt, and `model_routing` artifact
records. It must not rerun selector policy, read provider credentials, make
network calls, or create a new provider authority.

Operators can also inspect the selector output before dispatching a run:

```bash
odin work route-preview --task <task-id-or-key> [--json]
```

Route preview reads existing Task and project policy state, derives the same
portable routing fields dispatch would use, and returns the selected route,
executor lane, provider, model, fallback flag, policy reason, and deterministic
requirement inputs. It must not append runtime events, create a Run Attempt,
record artifacts, prepare leases, read provider credentials, or make network
calls.

OpenRouter fixture execution may also record `executor_evidence` fields that
prove request construction without network access. Stable fields include
`openrouter_request_constructed`, `openrouter_request_sha256`,
`openrouter_request_json_redacted`, `network_access`, and `fixture_transport`.
The redacted request proof must preserve only non-secret request shape such as
model id, message roles, message content hashes and byte counts, max token
settings, streaming flag, and redacted headers.

The approval-gated live smoke path records `openrouter_live_smoke_request` on
the prepare Run Attempt with `network_access=false`, then records
`openrouter_live_smoke_result` on the live Run Attempt only after the matching
Approval Request is approved and `--live --confirm-live-provider-call` are
present. These artifacts may record provider/model ids, request hash, response
id, latency, and token counts, but must not record `OPENROUTER_API_KEY`, bearer
tokens, raw prompt content, or raw provider error bodies.

`odin provider openrouter smoke evidence` is a read-only projection over those
same artifacts and events. It may summarize approval id, prepare/live run ids,
artifact counts, event counts, token counts, latency, and redaction scan booleans
such as `secret_leak_detected=false`, but it must not persist new evidence or
replay provider calls.

## Provenance trail expectation

The SQLite event stream is also the canonical source for operator-facing provenance trails. `odin logs` remains the raw event listing surface. `odin logs show <event-id>` and `odin logs trail --task <id|key>`, `--run <id>`, or `--approval <id>` are read-only projections over the same events table.

Trail rendering may enrich events with existing project and work item identifiers for readability, but it must not create a second event bus, audit table, dashboard-specific evidence store, or synthetic lifecycle authority. JSON trail output may include raw event payloads so operators can inspect the durable evidence behind the human-readable summary.

Read-only provenance commands and `/overview` Activity Log rendering must not append runtime events.

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

## Memory proposal expectation

Durable memory writes that are not already accepted runtime facts must enter
Odin as reviewable Memory Proposals. `odin memory propose` records a
`memory_summaries` row with `details_json.schema=memory_proposal.v1`, pending
status, explicit scope, source/provenance fields, and safety classification.

Proposal creation appends:

- `memory.proposal_created`

Proposal resolution through either `odin memory resolve` or
`odin review act memory-proposal:<id> ...` appends:

- `memory.proposal_resolved`

Payloads must identify the memory summary ID, scope, scope key, memory type,
proposal status, decision when resolved, source type, source ID or key,
sensitivity, reviewer when present, and review reason when present. They must
not include raw sensitive content.

Pending, rejected, and archived Memory Proposals are audit records only. Normal
active-memory recall must exclude them unless a command explicitly asks for that
proposal status.

## Intake-to-goal expectation

Raw intake processing remains on the `intake_items` SQLite authority. When deterministic processing routes a raw intake item into a reviewable goal, Odin must preserve the intake-to-goal link on the intake row, leave the goal unapproved, and append audit events through the runtime event stream:

- `intake.processed`
- `intake.routed_to_goal`
- `goal.created`

The processing payload must include the source intake ID, route decision, classification result, and created goal ID when a goal is created. Intake conversion must not approve, run, or mutate external systems.

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
- `browser.profile_encrypted`
- `browser.profile_attach_requested`
- `browser.profile_attached`
- `browser.profile_attach_failed`
- `browser.profile_attach_cleaned`
- `browser.profile_revoked`
- `browser.profile_expired`
- `browser.profile_cleaned`
- `browser.profile_cleanup_failed`
- `browser.profile_materialized`
- `browser.profile_materialization_cleaned`
- `goal.waiting_for_human_login`

Browser session events must not include passwords, cookies, bearer tokens, passkey material, TOTP values, backup codes, profile bytes, or raw credential prompts. Login request events may include a log-safe opaque `handoff_id` and a metadata-only `handoff_url`; neither proves that a handoff HTTP route exists. Metadata-only session verification records operator-attested verification and `last_verified_at`; browser-observed account/domain verification remains future work. Profile preparation records only empty-directory preparation metadata plus `profile_storage_policy`; a prepared directory is not approval to write browser files. Encrypted profile artifact and attach events record only safe metadata such as artifact ID, session ID, runner ID, relative encrypted artifact path, key reference, relative materialization path, attach status, policy decision, actor, reason, and safe error code/message; they must not include fixture plaintext, key bytes, source fixture path, cookie values, credential stores, or browser profile bytes. Session verification may unblock a waiting goal only through normal policy checks; it must not approve or execute the goal by itself.

`odin browser session handoff show --handoff-id <id>` is intentionally read-only. It validates the handoff ID, login request status, expiration, and linked session status, but emits no runtime event because it performs no state change.

## Automation trigger expectation

Automation Trigger events are the audit trail for scheduled and event-backed work creation. Trigger definitions and evaluations remain in SQLite; registry prompts, YAML policy, and design docs do not count as real automation unless a real `odin trigger` or `odin scheduler` command invokes the path.

Trigger mutation events should preserve enough evidence to reconstruct:

- trigger key and workspace
- trigger source such as `schedule`, `event`, `manual`, or `test`
- deterministic `materialization_key`
- event envelope with `source`, `trigger_type`, `dedupe_key`, `occurred_at`, and `recovery_state`
- due time or source-event time when applicable
- execution intent and approval-required posture when present
- linked Work Item when a real evaluation materializes work

`automation_trigger.tested` is allowed for operator preview commands. It records preview/audit evidence with `mutates=false`, but it must not create Work Items, materialization rows, Run Attempts, Approval Requests, external adapter mutations, or dispatches.
