---
title: Audit Logs, Events, And Runtime Evidence Design
date: 2026-05-10
status: approved-for-implementation-planning
scope: odin-os provenance trail v1, human-readable evidence readback
---

# Audit Logs, Events, And Runtime Evidence Design

## Purpose

Every meaningful Odin action should leave durable evidence that an operator can
read without reverse-engineering raw JSON. Odin already has the audit spine:
SQLite events are canonical, event payloads are typed, and `odin logs --json`
can expose the raw stream. The hardening gap is operator readback and
cross-object correlation, not storage.

This slice adds a small provenance trail over existing runtime events. It does
not add a new audit table, message bus, event-sourcing framework, Loki
authority, or dashboard-specific evidence store.

## Audit Summary

Inspected:

- `/home/orchestrator/odin-os/AGENTS.md`
- `README.md`
- `WORKFLOW.md`
- `CONTEXT.md`
- `docs/adr/0001-canonical-authority.md`
- `docs/architecture/ADR-0001-brownfield-refactor-strategy.md`
- `docs/contracts/runtime-events.md`
- `docs/contracts/observability.md`
- `docs/contracts/tui-overview.md`
- `docs/contracts/work-execution-state.md`
- `docs/contracts/failure-analysis.md`
- `docs/contracts/external-intake.md`
- recent commits through `origin/main`, including review action receipts,
  event-trigger proof, and the Work Item / Run Attempt state contract
- SQLite schema in `internal/store/sqlite/migrations/0001_runtime.sql`
- event model in `internal/runtime/events/events.go`
- event persistence in `internal/store/sqlite/store.go`
- event replay and projections in `internal/runtime/projections/projections.go`
- CLI lifecycle routing in `internal/app/lifecycle/run.go`
- shell log handling in `internal/cli/repl/shell.go`
- overview service/rendering in `internal/cli/overview/service.go` and
  `internal/cli/render/overview.go`
- existing tests around logs, intake audit evidence, approvals, triggers,
  jobs/runs, review action receipts, overview, and event replay
- installed command surface through `which odin`, `realpath "$(which odin)"`,
  `odin help`, and `odin logs --help`
- repo-local command surface through `go run ./cmd/odin logs --help` and
  `go run ./cmd/odin overview --help`

Live command findings:

```bash
which odin
realpath "$(which odin)"
odin help
odin logs --help
go run ./cmd/odin logs --help
go run ./cmd/odin overview --help
```

The installed operator binary resolves through `/home/orchestrator/.local/bin/odin`
to `/home/orchestrator/odin-os/releases/current/bin/odin`. The current logs
surface is `odin logs [--json]`. The current overview surface is
`odin overview [--json]`.

Fresh-root probe:

```bash
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" go run ./cmd/odin work start --project odin-core --title "evidence design approval probe" --intent governance
ODIN_ROOT="$tmp" go run ./cmd/odin work dispatch --task 1 --json
ODIN_ROOT="$tmp" go run ./cmd/odin logs
ODIN_ROOT="$tmp" go run ./cmd/odin logs --json
ODIN_ROOT="$tmp" go run ./cmd/odin overview
```

Observed:

- governance-intent dispatch blocks with `blocked_reason=approval_required`
- the event stream records `project.created`, `task.created`,
  `approval.requested`, `task.status_changed`, `task.queue_state_changed`,
  and `context_packet.created`
- `odin logs --json` exposes useful raw evidence, including `project_id`,
  `task_id`, payload JSON, execution intent, approval state, and blocked reason
- text `odin logs` only renders `id type scope`, which is audit-complete but not
  human-readable
- `/overview` surfaces Attention, Review Queue, Active Execution, Work Items,
  Approvals, Observability, Intake Inbox, and Automation Triggers, but it does
  not provide a general recent Activity Log

## Existing State

Odin already has first-class audit foundations:

- SQLite is the canonical runtime authority.
- `events` is an append-only audit stream with `stream_type`, `stream_id`,
  `event_type`, `event_version`, `scope`, optional `project_id`, optional
  `task_id`, optional `run_id`, `payload_json`, and `occurred_at`.
- ADR-0001 names audit events as canonical and rejects file-backed authority.
- `docs/contracts/runtime-events.md` defines event envelope and typed event
  expectations.
- `internal/runtime/events` defines typed stream names, event names, payloads,
  and encode/decode helpers.
- `internal/store/sqlite` appends events transactionally for many runtime
  mutations.
- `internal/runtime/projections.ReplayLifecycle` can replay core lifecycle
  state for tasks, runs, approvals, and follow-ups.
- `odin logs --json` exposes raw events.
- The REPL `/logs` command exposes recent event rows.
- `odin overview` already renders many adjacent operator lanes.
- `odin tui` consumes shared telemetry and Loki as observability consumers, not
  runtime authority.

Important existing event families include:

- work execution: `task.created`, `task.dispatch_requested`,
  `task.status_changed`, `task.queue_state_changed`, `task.retry_evaluated`,
  `task.recovery_recommended`, `run.started`, `run.execution_claimed`,
  `run.status_changed`, `run.finished`
- approvals and review: `approval.requested`, `approval.resolved`,
  `review.approved`, `review.rejected`
- intake: `intake.item_created`, `intake.processing_started`,
  `intake.classified`, `intake.dedupe_reviewed`, `intake.routed`,
  `intake.processed`, review and approval decision events
- triggers and scheduler: `automation_trigger.*`, `scheduler.tick_evaluated`,
  `external.github.issue`
- failure evidence: `run.finished` artifacts, `task.retry_evaluated`,
  `task.recovery_recommended`, `recovery.*`, `incident.*`
- skills, browser, goals, delegations, memory, context packets, capability
  snapshots, learning, and project transitions

## Partial Or Contradictory State

The audit trail is implemented, but operator readback is uneven:

- `odin logs --json` is rich enough for machines but too raw for quick human
  provenance checks.
- text `odin logs` renders only event ID, type, and scope.
- `odin logs` has no detail command.
- `odin logs` has no trail command for a Work Item, Run Attempt, Approval
  Request, intake item, trigger, or review queue item.
- `ListEventsParams` can filter by `project_id`, `task_id`, and `run_id`, but
  not directly by approval ID, stream, event type, recency, or limit.
- `/overview` has Observability and Skill Activity, but no general Activity Log
  section.
- Some event families carry strong explicit correlation fields such as
  `task_id`, `run_id`, `source_event_id`, `dedupe_key`, `materialization_key`,
  route results, approval status, and review IDs; others rely mostly on stream
  identity or payload-specific details.
- Loki and JSON service logs are observability consumers and operational logs,
  but they must not be treated as canonical audit authority.

## Reused Components

The implementation should reuse:

- `events` SQLite table
- `runtimeevents.Record`
- existing event payload types in `internal/runtime/events`
- `sqlite.Store.ListEvents`
- existing project/task/run/approval lookup helpers
- `odin logs [--json]`
- REPL `/logs`
- `odin overview [--json]`
- `overview.Service` and `render.RenderOverview`
- existing logs JSON envelope `commands.LogsView`
- existing `commands.LogView`
- `projections.ReplayLifecycle`
- review queue source adapters and review action receipt fields
- existing tests in `internal/app/lifecycle/run_test.go`,
  `internal/runtime/projections/replay_test.go`,
  `tests/integration/operator_overview_test.go`, and trigger/intake/review
  suites

## New Components

Add only a small read-model and operator presentation seam:

- `EventTrailView` / `EventTrailItem` or equivalent CLI view model
- `odin logs show <event-id> [--json]`
- `odin logs trail --task <id|key> [--json]`
- `odin logs trail --run <id> [--json]`
- `odin logs trail --approval <id> [--json]`
- a recent `Activity Log` subsection under `/overview` Observability
- documentation updates to lock correlation rules and non-goals
- focused tests proving the trail and overview activity log use existing event
  truth

No new event storage, audit table, event bus, queue, log collector, or dashboard
authority is needed.

## Why New Components Are Necessary

The existing audit trail is mostly machine-readable. Operators need to answer
simple provenance questions quickly:

- What created this Work Item?
- Why did this run block or fail?
- Which approval was requested, and what changed after resolution?
- Which intake, trigger, or scheduler event led to this Work Item?
- What review action was taken and what durable state changed?
- What failed, what evidence was recorded, and what retry/follow-up guidance
  exists?

The new read-model is necessary because dumping raw event JSON shifts too much
correlation work onto the operator. The implementation should summarize the
important evidence while preserving a JSON mode that exposes raw event
identifiers and payloads for scripts.

## Locked Domain Decisions

- Canonical audit authority: SQLite `events`.
- Canonical event envelope: the existing `runtimeevents.Record`.
- Canonical human command family: `odin logs`.
- Canonical cross-object dashboard consumer: `odin overview` Observability.
- `Activity Log` is a read-only projection of runtime events.
- `Provenance Trail` is a read-only correlated view over runtime events.
- JSON service logs and Loki are observability signals, not canonical audit
  authority.
- `source_event_id`, `task_id`, `run_id`, `approval_id`, stream identity,
  review queue ID, dedupe key, and materialization key are correlation fields;
  they are not new runtime state.
- Missing correlation must render explicitly as `unknown` or `not_linked`, not
  be inferred beyond evidence.
- Older events do not need backfill for v1. Weak links should be surfaced as
  remaining risks or follow-up candidates.
- No ADR is required. This is an additive read-model over the existing canonical
  event authority.

## Selected Design

Implement Provenance Trail V1 as a thin read model over existing runtime events.

### Command Surface

Keep current behavior:

```bash
odin logs
odin logs --json
```

Add:

```bash
odin logs show <event-id> [--json]
odin logs trail --task <id|key> [--json]
odin logs trail --run <id> [--json]
odin logs trail --approval <id> [--json]
```

Text output should be compact and readable. Each trail item should include:

- event ID
- timestamp
- event type
- scope
- object links when present: project, work item, run attempt, approval
- operator summary
- source/cause fields when present
- decision/result fields when present
- blocked/failure/retry guidance when present

JSON output should include:

- normalized trail metadata
- raw `event_id`, `stream_type`, `stream_id`, `event_type`, `scope`,
  `project_id`, `task_id`, `run_id`, `occurred_at`
- resolved object labels when available
- summary fields
- raw payload JSON

The trail command should fail closed on invalid references:

- unknown event ID: `event not found`
- unknown task/run/approval: existing lookup error or a stable not-found error
- unsupported argument combination: usage error
- no matching events: success with `no logs` text or empty JSON trail

### Correlation Rules

Use evidence in this order:

1. direct event envelope IDs: `task_id`, `run_id`, `project_id`
2. direct source object lookup: approval row links to task/run
3. payload fields such as `task_id`, `run_id`, `source_event_id`,
   `source_event_type`, `materialization_key`, `dedupe_key`, `work_item_id`,
   `work_item_key`, `review_id`, `queue_id`, and `goal_id`
4. stream identity when it is explicit and stable

Do not infer links from similar titles, timestamps, free-text summaries, or
non-unique labels.

### Overview Activity Log

Add an `Activity Log` subsection under the existing `Observability` section in
`odin overview`.

The v1 overview activity log should:

- render the most recent relevant runtime events for the current scope
- default to a small fixed limit, such as 5
- use the same summarization function as `odin logs trail`
- include event ID, type, linked work item/run/approval when available, and a
  short summary
- remain read-only
- not replace the existing Review Queue, Approvals, Work Items, or Run Attempts
  sections

### Summary Shape

The summarizer should have explicit handling for at least:

- `task.created`
- `task.status_changed`
- `task.queue_state_changed`
- `run.started`
- `run.execution_claimed`
- `run.finished`
- `approval.requested`
- `approval.resolved`
- `review.approved`
- `review.rejected`
- `intake.*` processing, routing, dedupe, review, and approval events
- `automation_trigger.fire_requested`
- `automation_trigger.evaluated`
- `automation_trigger.materialized`
- `automation_trigger.deferred`
- `automation_trigger.errored`
- `scheduler.tick_evaluated`
- `recovery.*`
- `incident.*`

Unknown event types should still render with event ID, type, scope, timestamp,
and raw-payload availability.

## Rejected Alternatives

Rejected: add a new `audit_trails` table.

Reason: `events` is already the canonical append-only audit stream. Duplicating
events into another table would create reconciliation risk.

Rejected: make Loki or JSON service logs the provenance authority.

Reason: service logs are operational telemetry. They can help debugging, but
they are not transactionally related to runtime state.

Rejected: build a separate dashboard-specific provenance service.

Reason: `/overview`, `odin logs`, and the future TUI/web surfaces should share
one read model over existing event truth.

Rejected: require a broad payload normalization migration before improving
readback.

Reason: older or weaker event families can render as partially linked. The
first slice should expose the gaps rather than block on a large migration.

## Test And Verification Plan

Local proof should include:

```bash
go test ./internal/app/lifecycle ./internal/cli/overview ./internal/cli/render ./internal/runtime/projections -run 'Test(Log|Logs|Overview|Activity|Replay|Review|Trigger|Approval)' -count=1
go test ./tests/integration -run 'TestOperatorOverviewUsesCanonicalBoard|TestWorkExecutionStateContract|TestFollowUpAcceptance' -count=1
make build
which odin && realpath "$(which odin)"
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin work start --project odin-core --title "evidence trail proof" --intent governance
ODIN_ROOT="$tmp" ./bin/odin work dispatch --task 1 --json
ODIN_ROOT="$tmp" ./bin/odin logs
ODIN_ROOT="$tmp" ./bin/odin logs show 3
ODIN_ROOT="$tmp" ./bin/odin logs trail --task 1
ODIN_ROOT="$tmp" ./bin/odin logs trail --approval 1
ODIN_ROOT="$tmp" ./bin/odin logs trail --task 1 --json
ODIN_ROOT="$tmp" ./bin/odin overview
```

Expected proof:

- text logs remain backward compatible enough for current usage
- `logs show` displays full event detail and raw payload availability
- task trail includes task creation, approval request, blocked status, queue
  state, and context packet evidence
- approval trail resolves approval-to-task linkage
- JSON trail includes raw payloads and normalized correlation metadata
- overview renders an Activity Log under Observability
- no new events are emitted by read-only `logs`, `logs show`, `logs trail`, or
  `overview`

## Documentation Changes

Update:

- `docs/contracts/runtime-events.md` with provenance trail expectations and
  correlation rules
- `docs/contracts/observability.md` with Activity Log as an Observability
  projection over SQLite events
- optionally `docs/contracts/tui-overview.md` if overview language needs to
  name Activity Log as part of the operator board

No `CONTEXT.md` change is required because this slice does not introduce new
domain ownership. No ADR is required because the decision follows ADR-0001.

## Open Blockers

None for the first implementation slice.

Known follow-up risks:

- Some older event families may not include enough direct correlation to render
  full trails.
- `ListEventsParams` may need a small additive filter or limit field for
  efficient readback, but it should remain a store read helper, not a new audit
  authority.
- Future web/TUI provenance views should reuse the same summarizer rather than
  inventing separate event formatting.

## Planning Handoff

Implement as a PR-sized read-model slice:

1. Add failing tests for `odin logs show`, `odin logs trail`, and overview
   Activity Log using an approval-blocked work item fixture.
2. Add a reusable event summarizer/read model near the CLI/overview boundary.
3. Extend `runLogs` parsing while preserving `odin logs` and
   `odin logs --json`.
4. Add overview model fields and renderer lines for recent Activity Log.
5. Update runtime/observability docs.
6. Run local proof and real repo-built `./bin/odin` smoke.
7. Open a PR with Summary, Proven, Unproven, Security Review, and Commands Run.
8. Monitor checks, fix failures in follow-up atomic commits, and merge only if
   checks pass and repo policy permits.

## Implementation Goal Prompt

```text
/goal Implement Provenance Trail V1 in /home/orchestrator/odin-os.

Use the approved design at docs/superpowers/specs/2026-05-10-audit-logs-events-runtime-evidence-design.md. Keep the work PR-sized. Make atomic commits that each leave the repo coherent. Reuse existing SQLite events, runtimeevents.Record, sqlite.Store.ListEvents, odin logs, odin overview, overview.Service, render.RenderOverview, review/approval/job/run/intake/trigger event payloads, and existing integration helpers. Do not introduce a new audit table, event bus, Loki authority, dashboard-specific evidence store, or broad event migration.

Required proof:
- go test ./internal/app/lifecycle ./internal/cli/overview ./internal/cli/render ./internal/runtime/projections -run 'Test(Log|Logs|Overview|Activity|Replay|Review|Trigger|Approval)' -count=1
- go test ./tests/integration -run 'TestOperatorOverviewUsesCanonicalBoard|TestWorkExecutionStateContract|TestFollowUpAcceptance' -count=1
- make build
- which odin && realpath "$(which odin)"
- tmp="$(mktemp -d)" and with that ODIN_ROOT prove ./bin/odin work start + work dispatch for governance approval blocking, ./bin/odin logs, ./bin/odin logs show <event-id>, ./bin/odin logs trail --task 1, ./bin/odin logs trail --approval 1, ./bin/odin logs trail --task 1 --json, and ./bin/odin overview Activity Log readback

Delivery:
- open a PR with Summary, Proven, Unproven, Security Review, and Commands Run
- monitor checks
- fix failures in follow-up atomic commits
- merge only if checks pass and repo policy permits
```
