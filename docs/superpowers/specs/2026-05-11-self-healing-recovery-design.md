---
title: Self-Healing And Recovery Design
date: 2026-05-11
status: approved-for-implementation-planning
scope: odin-os recovery operator review v1
---

# Self-Healing And Recovery Design

## Purpose

Failed automation should produce visible failure state, bounded recovery
evidence, and operator-safe next steps. It must not silently retry until it
burns the retry budget, mutates policy, or hides an ambiguous failure behind a
generic playbook result.

This slice hardens Odin's existing recovery subsystem by making recovery
decisions explicit, reviewable, and visible through the canonical operator
surfaces.

## Audit Summary

Inspected:

- `AGENTS.md`
- `README.md`
- `WORKFLOW.md`
- `CONTEXT.md`
- `docs/contracts/self-heal.md`
- `docs/contracts/failure-analysis.md`
- `docs/contracts/work-execution-state.md`
- `docs/contracts/github-tracker-mutations.md`
- `docs/contracts/tui-overview.md`
- `docs/plans/2026-04-09-phase-11-self-heal-design.md`
- `internal/runtime/recovery/*`
- `internal/runtime/projections/projections.go`
- `internal/store/sqlite/store.go`
- `internal/store/sqlite/models.go`
- `internal/app/lifecycle/review.go`
- `internal/app/lifecycle/review_sources.go`
- `internal/cli/overview/service.go`
- `internal/cli/render/overview.go`
- `registry/skills/failure-analysis.md`
- `registry/agents/audit-log-summarizer-agent.md`
- `registry/agents/software-feature-ticket-builder-agent.md`
- `config/policies.yaml`
- `config/odin.yaml`
- `config/projects.yaml`

Commands run:

```bash
git status --short --branch
rg -n "self-heal|recovery|failure_analysis|failed-work|playbook|retry|approval_required|ticket" README.md WORKFLOW.md CONTEXT.md docs internal tests registry config scripts
make build
which odin && realpath "$(which odin)"
ODIN_ROOT="$(mktemp -d)" ./bin/odin help
ODIN_ROOT="<temp>" ./bin/odin review list --json
ODIN_ROOT="<temp>" ./bin/odin overview
ODIN_ROOT="<temp>" ./bin/odin logs --json
```

Real command evidence:

- `make build` succeeded on the isolated design worktree.
- Installed `odin` resolved to `/home/orchestrator/odin-os/releases/current/bin/odin`.
- Fresh `ODIN_ROOT` readbacks showed the current operator surfaces:
  - `odin review list --json` returned an empty unified review queue.
  - `odin overview` rendered `Review Queue` and `Observability`.
  - `Observability` already includes `Activity Log`, `Run Attempts`,
    `Blocked Work`, `Incidents`, and `Recoveries`.
  - `odin logs --json` returned the empty event stream for the fresh root.

## Existing State

Implemented or partial:

- `internal/runtime/recovery` has typed fault keys for:
  - `executor_health_stale`
  - `projection_stale`
  - `source_freshness_stale`
  - `queue_pressure_high`
  - `run_failure_repeated`
- Recovery monitors observe existing runtime state.
- Diagnosis maps known faults to code-defined playbooks.
- Built-in playbooks can refresh executor health, refresh projection
  freshness, reload registry source, checkpoint repeated failed runs, and
  escalate queue pressure.
- The executor creates or reuses incidents, starts recovery rows, records
  `recovery.action_executed`, completes or escalates recovery attempts, and
  respects cooldown and retry limits.
- `odin serve` runs the recovery cycle in a bounded background loop.
- Failed Work Items surface in `odin review` as `failed-work:<task-id>`.
- Failed-work review supports bounded `retry` and approval-style `follow-up`
  creation through an internal Follow-Up Obligation.
- `docs/contracts/failure-analysis.md` defines advisory failure analysis and
  forbids auto-applying workflow changes.
- `docs/contracts/github-tracker-mutations.md` blocks direct GitHub follow-up
  issue creation until an approved tracker-mutation bundle exists.
- `/overview` already shows incidents, recoveries, recovery guidance, review
  counts, and recent runtime events.

Partial or contradictory:

- `CONTEXT.md` already locks recovery decision modes such as `ignore`,
  `incident_only`, `playbook`, and `escalate`, but current code still models a
  decision primarily as `Decision{Playbook string}`.
- `CONTEXT.md` says ignored observations should remain explicit decisions, but
  current diagnosis drops observations that do not map to playbooks.
- `CONTEXT.md` says `incident_only` should open or reuse an incident without
  creating a recovery row, but the current executor only handles playbook-shaped
  decisions.
- `CONTEXT.md` says executor outcomes should use typed statuses and avoid
  redundant booleans, but current `Outcome` uses raw strings plus `Suppressed`
  and `Escalated` booleans.
- `CONTEXT.md` says playbook `ActionResult.Status` should be closed to
  `completed`, `failed`, and `escalated`, but current code accepts other values
  through the default failure/escalation branch.
- Recovery rows in `/overview` are thin. Operators see recovery id, run, status,
  strategy, and start time, but not fault key, subject, action name, decision
  mode, or next operator action.
- Incidents without a run are supported in store/executor paths but weaker in
  projection joins, which currently join incidents through runs/tasks/projects.
- There is no explicit `recovery` review source in the unified review queue.
  Failed work appears, but risky recovery action proposals do not have their own
  first-class review item.

Missing:

- Explicit manual approval path for risky recovery actions.
- Better dashboard surfacing for recovery decisions and next actions.
- A failure-to-ticket workflow that can produce a ticket-shaped proposal without
  directly creating a GitHub issue.
- More recovery playbooks, gated by risk and decision mode.

## Reused Components

- SQLite runtime authority: `incidents`, `recoveries`, `events`, tasks, runs,
  approvals, follow-up obligations, and context packets.
- `internal/runtime/recovery` monitor, diagnosis, playbook, executor, service,
  failure-analysis, and retry-guidance packages.
- `runtimeevents.Record` and existing runtime event persistence.
- `odin review` unified review queue source pattern.
- `odin overview` and `internal/cli/overview.Service`.
- `internal/cli/render.RenderOverview`.
- Existing failed-work review actions and Follow-Up Obligation persistence.
- Existing approval policy posture in `config/policies.yaml`.
- Existing authored registry assets for failure analysis, audit summarization,
  and software feature ticket shaping.
- GitHub tracker mutation contract, which keeps external issue writes manual
  until a separate approved mutation bundle exists.

## New Components

New implementation components for this slice:

1. Typed recovery decision model:
   - `DecisionMode` with `ignore`, `incident_only`, `playbook`,
     `approval_required`, and `escalate`.
   - `OutcomeStatus` with `incident_only`, `completed`, `failed`,
     `suppressed`, and `escalated`.
   - Closed `ActionResultStatus` with `completed`, `failed`, and `escalated`.

2. Recovery review proposal model:
   - a review queue source for risky recovery actions, exposed as
     `recovery:<incident-id>` or `recovery:<proposal-id>` depending on the
     minimal schema chosen during implementation.
   - review detail with fault key, subject key, severity, proposed action,
     risk, evidence, allowed actions, and next steps.

3. Recovery overview read model:
   - enriched incident and recovery summaries with fault key, subject key,
     decision mode, action name, approval/review state, and next action.

4. Failure-to-ticket proposal path:
   - an internal ticket-shaped proposal derived from failure analysis or
     recovery review evidence.
   - no direct GitHub issue creation in this slice.

5. First incident-only hardening fault:
   - `wake_packet_invalid`, only if the implementation can reuse existing
     checkpoint decode/error seams without widening the slice.

## Why New Components Are Necessary

Typed decision and outcome models are necessary because current playbook-shaped
diagnosis cannot represent human-attention-only recovery without either
dropping the observation or fabricating a recovery row.

Recovery review proposals are necessary because some recovery actions are
useful but risky. They need the same operator review discipline as approvals,
failed work, intake review, memory proposals, and skill artifacts.

Overview enrichment is necessary because "recovery exists" is not enough for
operators. The dashboard needs to answer: what failed, what Odin decided, what
ran or did not run, what evidence exists, and what a human should do next.

The failure-to-ticket proposal path is necessary because the current Follow-Up
Obligation path captures a next action, but not a full ticket-shaped artifact
with acceptance criteria, affected systems, risks, and verification needs.

`wake_packet_invalid` is a good first incident-only candidate because the domain
decision is already locked in `CONTEXT.md`: invalid wake-envelope corruption
should open or reuse an incident and stop at human attention required, not run a
speculative repair playbook.

## Locked Domain Decisions

- Self-heal is deterministic recovery, not self-improvement.
- Recovery must not mutate governance policy, approval policy, manifests, or
  executor routing config.
- Failed automation must remain visible through Work Item, Run Attempt,
  incident, recovery, event, review, and overview surfaces.
- Retry behavior must stop at configured limits.
- Failure analysis is advisory and guardrail-preserving.
- Ticket-readiness failures outrank implementation retry recommendations.
- Risky recovery actions require explicit operator review before mutation.
- External GitHub issue creation remains blocked until the GitHub tracker
  mutation contract is implemented and the exact write is approved.
- `playbook` remains a recovery term. Human-facing controls are Operator
  Surfaces; documentation for humans may be a runbook.
- `recoveries` represent deterministic recovery attempts. They must not be used
  as generic annotations for incident-only decisions.
- `incident_only` opens or reuses an incident and stops without creating a
  recovery attempt.
- Ignored observations should remain visible in the decision stream and logs,
  but should not flow through the executor as fake outcomes.

## Selected Design

### Operator Workflow

1. A runtime fault is observed by recovery monitors.
2. Diagnosis emits an explicit decision for every observation:
   - `ignore`: no action, logged as a diagnosis decision.
   - `incident_only`: open or reuse an incident and stop.
   - `playbook`: run a bounded deterministic playbook.
   - `approval_required`: create or update a recovery review proposal.
   - `escalate`: open or update an escalated incident without retrying.
3. The executor handles only side-effecting decisions:
   - `incident_only`
   - `playbook`
   - `approval_required`
   - `escalate`
4. Safe playbooks may run automatically within cooldown and retry limits.
5. Risky playbooks become recovery review items.
6. Operators inspect recovery items through:
   - `odin review list`
   - `odin review show recovery:<id>`
   - `odin overview`
   - `odin logs trail --approval|--task|--run` where linked evidence exists
7. Operators may:
   - approve a safe follow-up/ticket proposal
   - reject the proposal
   - request clarification
   - create an internal Follow-Up Obligation
   - keep the incident open
8. External GitHub ticket creation remains proposal-only.

### Decision Model

`Decision` should carry:

- `Mode`
- `Observation`
- optional `Playbook`
- optional `ActionName`
- `Risk`
- `Reason`
- `RequiresApproval`
- `SuggestedReviewAction`

Diagnosis should return explicit decisions for all observations. Unknown or
unsupported observations return `ignore` with a reason, not a missing entry.

### Outcome Model

`Outcome` should carry:

- `Status`
- `DecisionMode`
- optional `Incident`
- optional `Recovery`
- optional `Attempt`
- `FaultKey`
- `SubjectKey`
- `Reason`

Remove redundant outcome booleans once callers are migrated to typed status.

### Approval And Review Path

This slice should reuse `odin review`, not create `odin recovery approve`.

Recovery review items should show:

- review id
- fault key
- subject key
- scope
- severity
- incident id
- optional recovery id
- proposed playbook/action
- risk
- reason approval is required
- evidence summary
- next steps
- allowed actions

For V1, allowed actions should be conservative:

- `follow-up`
- `reject`
- `clarify`

Approving direct execution of risky playbooks can be a later slice unless a
bounded local-only action can be proven without touching policy, production,
external services, or filesystem mutation outside `ODIN_ROOT`.

### Dashboard Surfacing

`/overview` should enrich Observability rows:

- Incidents:
  - `fault_key`
  - `subject_key`
  - `decision_mode`
  - `review_state`
  - `next_action`
- Recoveries:
  - `fault_key`
  - `subject_key`
  - `action_name`
  - `attempt`
  - `result`
  - `decision_mode`
- Review Queue:
  - count recovery review proposals separately or include them in a generic
    `recovery_count` if adding a field is less disruptive.

### Failure-To-Ticket Workflow

Failed automation should be able to produce an internal ticket-shaped proposal
without directly creating an external issue.

V1 should:

- derive the proposal from failed-work detail, failure analysis, incident
  details, recovery action evidence, and logs trail references.
- include title, problem, proposed solution, acceptance criteria, affected
  systems, risks, test requirements, documentation requirements, and source
  evidence.
- surface it through `odin review show failed-work:<id>` or
  `odin review show recovery:<id>`.
- materialize only an internal Follow-Up Obligation or internal ticket proposal,
  not a GitHub issue.

### More Playbooks

More recovery playbooks should come after decision-mode hardening. Candidate
order:

1. `wake_packet_invalid`: `incident_only`; opens or reuses an incident and
   stops.
2. `approval_wait_stale`: review-required; proposes sealing stale wake packet
   only after operator confirmation.
3. `failed_trigger_materialization`: review-required; proposes disabling or
   editing trigger only after a separate policy-approved mutation contract.
4. `runtime_projection_repair`: safe playbook only when it recomputes a
   projection from SQLite authority without policy or external mutation.

## Rejected Alternatives

### Add More Automatic Playbooks First

Rejected because more playbooks increase the automation surface before the
decision and review model can distinguish safe deterministic repair from risky
operator-governed action.

### Create A Separate Recovery Dashboard

Rejected because Odin already has canonical operator surfaces: `odin review`,
`odin overview`, `odin logs`, incidents, recoveries, tasks, and runs.

### Auto-Create GitHub Issues From Failure Analysis

Rejected because `docs/contracts/github-tracker-mutations.md` requires an
approved tracker mutation bundle before external follow-up issue creation.

### Treat Failure-To-Ticket As A Normal Follow-Up Obligation Only

Rejected because Follow-Up Obligations are useful next actions, but they do not
by themselves capture the ticket-shaped fields needed for repair planning and
acceptance.

### Keep Raw String Recovery Statuses

Rejected because the domain model has already locked typed recovery decision
and outcome vocabularies. Raw strings let invalid playbook output collapse into
ordinary failure handling.

## Test And Verification Plan

Implementation should start with failing tests.

Focused unit tests:

```bash
go test ./internal/runtime/recovery -run 'TestDiagnos|TestExecutor|TestRecoveryReview|TestWakePacketInvalid' -count=1
go test ./internal/store/sqlite -run 'TestRecovery|TestSelfHeal|TestReview' -count=1
go test ./internal/cli/overview ./internal/cli/render -run 'TestOverview|TestRecovery|TestReview' -count=1
go test ./internal/app/lifecycle -run 'TestReview|TestRecovery|TestFailedWork|TestLogs' -count=1
```

Integration tests:

```bash
go test ./tests/integration -run 'TestOperatorOverviewUsesCanonicalBoard|TestWorkExecutionStateContract|TestFollowUpAcceptance' -count=1
```

Build and command proof:

```bash
make build
which odin && realpath "$(which odin)"
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin review list --json
ODIN_ROOT="$tmp" ./bin/odin overview
ODIN_ROOT="$tmp" ./bin/odin logs --json
```

Fresh-root proof should demonstrate at least one seeded or command-created
failed automation scenario where:

- failed state is visible
- retry guidance is visible
- retry does not exceed the configured budget
- risky recovery appears in review instead of executing
- ticket/follow-up proposal is inspectable
- overview mirrors the same state
- logs trail includes the recovery or review evidence

## Documentation Changes

Implementation should update:

- `docs/contracts/self-heal.md`
- `docs/contracts/failure-analysis.md`
- `docs/contracts/tui-overview.md`
- `docs/contracts/work-execution-state.md` only if state proof wording changes
- `CONTEXT.md` only if new domain decisions are locked during implementation

No ADR is required for this slice because it follows already-locked recovery,
review, and SQLite-authority decisions rather than introducing a hard-to-reverse
architecture change.

## Open Blockers

No design blocker remains for a PR-sized implementation slice.

Implementation must still decide the minimal persistence shape for recovery
review proposals:

- prefer deriving review items from existing incident/recovery details first.
- add a new table only if derived state cannot represent review lifecycle
  cleanly.

Implementation must not add external GitHub issue mutation.

## Planning Handoff

Recommended first implementation slice:

1. Add typed decision and outcome vocabularies to `internal/runtime/recovery`.
2. Preserve existing behavior by mapping current playbook decisions to
   `Mode=playbook`.
3. Emit explicit `ignore` decisions for unknown observations.
4. Add `incident_only` executor handling without creating a recovery row.
5. Add `wake_packet_invalid` as an incident-only decision only if it can be
   wired without broad checkpoint/projection rewrites.
6. Enrich overview recovery/incident rows with fault key, subject key, decision
   mode, and next action.
7. Add recovery review items for approval-required risky actions if the first
   slice still fits; otherwise leave that as the next PR and keep this PR
   focused on the decision model plus overview surfacing.

Keep the PR small. Do not implement new external tracker mutation, broad
dashboard redesign, or dynamic authored playbooks.

## Implementation Goal Prompt

```text
/goal Implement Recovery Operator Review V1 foundation in /home/orchestrator/odin-os.

Use docs/superpowers/specs/2026-05-11-self-healing-recovery-design.md. Keep PR-sized and use atomic commits. Reuse internal/runtime/recovery, SQLite incidents/recoveries/events, odin review, odin overview, odin logs, existing failed-work retry/follow-up review, and existing integration helpers. Do not add a second recovery dashboard, new event bus, external GitHub issue creation, dynamic playbook interpreter, or policy mutation.

Required slice:
- Add typed recovery decision modes: ignore, incident_only, playbook, approval_required, escalate.
- Add typed executor outcome status and closed playbook ActionResult status validation.
- Preserve existing playbook behavior through Mode=playbook.
- Make diagnosis emit explicit ignore decisions for unsupported observations.
- Implement incident_only so it opens/reuses an incident and creates no recovery row.
- Add or wire wake_packet_invalid only as incident_only if it stays narrow.
- Enrich overview/review readbacks enough that operators can see fault_key, subject_key, decision mode, status, and next action.
- Keep risky recovery actions review-gated; if full recovery review proposal persistence is too large, document it as the immediate next slice and leave no automatic risky action path.

Required proof:
- go test ./internal/runtime/recovery -run 'TestDiagnos|TestExecutor|TestRecovery|TestWake' -count=1
- go test ./internal/app/lifecycle ./internal/cli/overview ./internal/cli/render -run 'TestReview|TestRecovery|TestOverview|TestLogs' -count=1
- go test ./tests/integration -run 'TestOperatorOverviewUsesCanonicalBoard|TestWorkExecutionStateContract|TestFollowUpAcceptance' -count=1
- make build
- which odin && realpath "$(which odin)"
- fresh ODIN_ROOT proof with ./bin/odin review list, ./bin/odin overview, ./bin/odin logs, and at least one recovery/failure scenario proving visible failure state and no silent retry loop.

Open a PR with Summary, Proven, Unproven, Security Review, and Commands Run. Monitor checks, fix failures in follow-up commits, and merge only if checks pass and repo policy permits.
```

## Completion Audit

Objective requirements mapped to artifact evidence:

| Requirement | Evidence in this spec |
| --- | --- |
| Failed automation should not silently retry itself into a crater | Purpose, Existing State, Selected Design, Test And Verification Plan |
| Visible failure state | Existing State, Dashboard Surfacing, Failure-To-Ticket Workflow |
| Self-healing recommendations | Existing State, Failure-To-Ticket Workflow, Overview enrichment |
| Not silent retry loops | Locked Domain Decisions, Test And Verification Plan |
| Current status partial | Audit Summary, Existing State, Partial or contradictory |
| Failure detection | Existing State lists monitors and fault keys |
| Recovery service cycles | Existing State lists `odin serve` recovery loop |
| Self-healing recommendations | Existing State and Failure-To-Ticket Workflow |
| Guardrail-preserving repair suggestions | Locked Domain Decisions and Rejected Alternatives |
| More recovery playbooks | More Playbooks section |
| Better dashboard surfacing | Dashboard Surfacing section |
| Clear manual approval path for risky recovery actions | Approval And Review Path section |
| Failure-to-ticket workflow | Failure-To-Ticket Workflow section |
| Written repo artifact | This file under `docs/superpowers/specs/` |
| Implementation-ready handoff | Planning Handoff and Implementation Goal Prompt |

Completion status: design objective satisfied. Implementation remains future
work covered by the goal prompt.
