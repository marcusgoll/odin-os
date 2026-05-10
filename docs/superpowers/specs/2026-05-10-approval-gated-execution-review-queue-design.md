---
title: Approval-Gated Execution Review Queue Design
date: 2026-05-10
status: approved-for-implementation-planning
scope: odin-os approval-gated execution v1, slice 1
---

# Approval-Gated Execution Review Queue Design

## Purpose

Approval-Gated Execution v1 hardens Odin's operator decision boundary before broadening real-world mutation capability. Odin must not send messages, mutate shared calendars, purchase, delete, deploy, alter production systems, change permissions, publish public content, or change financial, legal, or medical records unless the action is visible, reviewable, approval-gated where required, and auditable through Odin-owned operator surfaces.

This first slice locks `odin review` as the unified operator queue for governed decisions. It does not add new external mutations or new resolver authority.

## Audit Summary

Inspected:

- `/home/orchestrator/odin-os/AGENTS.md`
- `CONTEXT.md`
- `README.md`
- `WORKFLOW.md`
- `docs/contracts/tui-overview.md`
- `docs/contracts/operational-autonomy.md`
- `docs/contracts/capability-gateway.md`
- `docs/contracts/executor-contract.md`
- `docs/contracts/runtime-events.md`
- `docs/contracts/external-intake.md`
- `docs/superpowers/specs/2026-05-10-universal-intake-system-design.md`
- `docs/superpowers/specs/2026-05-10-classification-dedupe-routing-design.md`
- `docs/plans/2026-05-09-odin-os-governed-operating-system.md`
- `internal/app/lifecycle/review.go`
- `internal/runtime/approvals/service.go`
- `internal/runtime/jobs/service.go`
- `internal/runtime/triggers/service.go`
- `internal/skills/policy.go`
- `internal/store/sqlite/migrations/0001_runtime.sql`
- `internal/store/sqlite/store.go`
- current installed command surface through `odin help`, `odin review --help`, and `odin approvals --help`

Live command surface found:

```bash
which odin
realpath /home/orchestrator/.local/bin/odin
odin help
odin review --help
odin approvals --help
```

The installed operator binary resolves through `/home/orchestrator/.local/bin/odin` to `/home/orchestrator/odin-os/releases/current/bin/odin`, and it exposes `approvals`, `review`, `intake`, `jobs`, `runs`, `trigger`, `knowledge`, and `overview`.

## Existing State

Odin already has the main primitives:

- `Approval Request` is the durable governance object linked to a Work Item and optionally a Run Attempt.
- `odin approvals` lists, shows, and resolves approval records, including resolver-aware unsupported refusal.
- `odin review` already lists multiple governed decision sources in one queue.
- `internal/runtime/approvals.Service` owns approval detail, resolver support, resolve refusal, task-backed resolution, and prepared-transfer continuation.
- `internal/runtime/jobs.Service` stores `execution_intent`, classifies fallback intent, blocks governance and destructive work with `approval_required`, and requests approval records through the store.
- `internal/runtime/triggers.Service` accepts trigger `execution_intent` values of `read_only`, `mutation`, `governance`, and `destructive`.
- `internal/skills.ResolveInvocationPolicy` marks governance and destructive permissions as approval-needed and rejects mutating skill permissions outside allowed scopes.
- SQLite stores approvals and appends `approval.requested` and `approval.resolved` events.
- Universal Intake designs already require raw intake and processing to stop before execution by default.

Current `odin review` source coverage includes:

- `intake_review`
- `intake_approval`
- `intake_goal_conversion`
- `goal`
- `goal_blocker`
- `task_approval`
- `skill_artifact`
- `context_pack`
- `failed_work`

## Reused Components

The implementation should reuse:

- `odin review list/show/approve/reject/act`
- `odin approvals all|supported|unsupported/show/resolve`
- `internal/app/lifecycle/review.go` entrypoint behavior and existing entry render helpers
- `internal/runtime/approvals.Service`
- `internal/runtime/jobs.Service` admission and retry behavior
- `internal/runtime/projections` pending approval and task status read models
- `runtimeknowledge.Service` context-pack proposal review
- existing intake review and intake approval commands
- existing skill artifact review behavior
- existing failed-work retry policy and `RetryFailedTaskFromReview`
- `docs/contracts/tui-overview.md` as the operator projection contract
- `docs/contracts/runtime-events.md` as the audit contract

## New Components

Add only a small review-source composition seam inside `internal/app/lifecycle`:

- `reviewQueueSource` interface with `Name()` and `List(...)`
- `listReviewQueueEntriesFromSources(...)`
- source structs for the existing source families
- characterization tests proving each governed decision source appears in `odin review`

No new queue table, approval table, event stream, daemon, adapter, or resolver package is needed for this slice.

## Why New Components Are Necessary

`odin review` is already the right queue, but the source inventory is concentrated in one lifecycle function. That makes it hard to prove that approvals, unclear intake, draft-ticket or goal conversions, failed work, skill artifacts, context-pack or memory-like proposals, and unsupported blockers all remain visible as the platform grows.

The new seam is necessary to make source coverage explicit and testable without changing operator behavior or creating a second review authority.

## Locked Domain Decisions

- Canonical operator decision surface: `odin review`.
- Canonical approval object: `Approval Request`.
- Canonical review item identity: `queue_id`.
- Canonical source type field: `source_type`.
- Canonical review action field: `allowed_actions`.
- Unsupported review items must remain visible and refuse mutation with a machine-readable `unsupported` and `not_resolved` result.
- Review queue filters or source composition do not authorize batch mutation.
- Every mutation remains a single explicit item action.
- `odin approvals` remains the approval-specific inspection and resolution surface, while `odin review` is the cross-source queue.
- Memory-like proposals in this slice mean existing `context_pack` proposals. Future durable memory write proposals must enter the same review source contract instead of creating a parallel memory queue.
- Failed automations are represented as `failed_work` until a narrower failed-automation source exists.
- Draft-ticket style items are represented through intake review, intake approval, and intake-to-goal conversion until a separate Work Item promotion source is implemented.

No ADR is needed for this slice. The decision is important, but it follows existing Odin contracts and does not introduce a hard-to-reverse architectural change.

## Selected Design

Keep `odin review` behavior-compatible and refactor the source list into explicit source providers.

Each review source must return entries with:

- `queue_id`
- `source_type`
- `status`
- `allowed_actions`
- enough object detail for `odin review show <queue-id> --json`

Source actions remain source-specific:

- intake review actions call existing intake review handlers.
- intake approval actions call existing intake approval handlers.
- task approval actions call existing approval resolution.
- skill artifact actions call existing skill artifact review.
- context-pack actions call existing knowledge context-pack review.
- failed-work retry calls existing jobs retry policy.
- unsupported goal blocker actions return unsupported/not-resolved without mutating the blocker or goal.

The contract addition belongs under `docs/contracts/tui-overview.md` in or near the Approvals and Attention lane language, because the review queue is an operator triage surface, not a new runtime authority.

## Rejected Alternatives

Rejected: create a new `review_queue` table.

Reason: current sources already have durable authorities. A new table would duplicate state and add synchronization risk.

Rejected: make `approvals` the only review queue.

Reason: approvals are only one governed decision source. Intake clarification, context-pack proposals, failed work, skill artifacts, and unsupported blockers are reviewable but not all are Approval Requests.

Rejected: add batch approve or batch reject.

Reason: batch mutation weakens the explicit human decision boundary and can hide resolver support, evidence review, and source-specific failure behavior.

Rejected: implement high-risk adapter mutation in this slice.

Reason: the first safety gap is the operator queue contract. External mutation should wait until queue visibility and action refusal are proven.

## Test And Verification Plan

Focused local tests:

```bash
go test ./internal/app/lifecycle -run 'TestReviewQueue|TestRunReview|UnifiedReview' -count=1
go test ./internal/runtime/approvals ./internal/runtime/jobs ./internal/runtime/knowledge -run 'Approval|Review|Retry|ContextPack' -count=1
```

Real operator proof after build:

```bash
which odin
realpath "$(which odin)"
make build
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin help
ODIN_ROOT="$tmp" ./bin/odin intake raw create --text "Review approval-gated execution queue contract" --json
ODIN_ROOT="$tmp" ./bin/odin intake raw list --json
ODIN_ROOT="$tmp" ./bin/odin review list --json
ODIN_ROOT="$tmp" ./bin/odin review show <queue-id> --json
```

Required proof conditions:

- `odin review list --json` emits valid JSON.
- every listed item has stable `queue_id`, `source_type`, `status`, and `allowed_actions`.
- unsupported items remain visible.
- unsupported actions return machine-readable refusal and do not mutate source state.
- no new Work Item, Run Attempt, dispatch, external send, deploy, purchase, delete, permission change, or public publish occurs during review inspection.

## Documentation Changes

Implementation should update:

- `docs/contracts/tui-overview.md` with a `Unified Review Queue` section.

Implementation may update:

- `CONTEXT.md` with `Unified Review Queue` if the implementer can do so without conflicting with existing dirty worktree changes.

No ADR is required for this slice.

## Security Review

This slice touches approval policy and operator governance surfaces. The implementation must preserve fail-closed behavior:

- no hidden resolver support
- no batch approval
- no mutation for unsupported source types
- no external adapter mutation
- no direct bypass around `approvals.Service` or source-owned handlers
- no claim of implementation until real `odin` proof shows the queue and refusal behavior

## Open Blockers

None for implementation planning.

The current worktree has unrelated dirty changes in `CONTEXT.md`, lifecycle, and REPL files. Implementation should start from an isolated worktree or explicitly coordinate with those changes before editing the same files.

## Planning Handoff

Implementation should be one PR-sized slice:

1. Add a failing review source inventory characterization test.
2. Introduce the review-source composition seam.
3. Move existing source loops behind source providers without changing output shape.
4. Document the unified review queue source/action contract.
5. Prove behavior through focused tests and real `odin` commands.

Do not implement policy parity, prompt-to-production, memory write proposals, or external adapter mutation in this slice. Those are follow-up slices after the queue contract is proven.

## Implementation Goal Prompt

```text
/goal Implement Approval-Gated Execution slice 1 in /home/orchestrator/odin-os.

Use the approved design at docs/superpowers/specs/2026-05-10-approval-gated-execution-review-queue-design.md. Keep the work PR-sized. Make atomic commits that each leave the repo coherent. Reuse existing odin review, odin approvals, internal/app/lifecycle review handlers, approvals.Service, jobs retry policy, knowledge context-pack review, SQLite approvals, and runtime events. Do not introduce parallel queues, approval tables, resolver packages, or batch approval.

Required proof:
- go test ./internal/app/lifecycle -run 'TestReviewQueue|TestRunReview|UnifiedReview' -count=1
- go test ./internal/runtime/approvals ./internal/runtime/jobs ./internal/runtime/knowledge -run 'Approval|Review|Retry|ContextPack' -count=1
- make build
- which odin && realpath "$(which odin)"
- ODIN_ROOT="$(mktemp -d)" ./bin/odin help
- with that ODIN_ROOT, create a raw intake item, list review items as JSON, and show one review item as JSON

Delivery:
- open a PR with Summary, Proven, Unproven, and Commands Run
- include a security review section because this touches approval/operator governance
- monitor checks
- fix failures in follow-up atomic commits
- merge only if checks pass and repo policy permits
```
