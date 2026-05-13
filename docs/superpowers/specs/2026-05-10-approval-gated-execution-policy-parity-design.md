---
title: Approval-Gated Execution Policy Parity Design
date: 2026-05-10
status: approved-for-implementation-planning
scope: odin-os approval-gated execution v1, slice 3
---

# Approval-Gated Execution Policy Parity Design

## Purpose

Approval-Gated Execution v1 hardens Odin's real-world mutation boundary. Odin must not send messages, change calendar events involving others, purchase, delete, deploy, alter production systems, change permissions, publish public content, or change financial, legal, or medical records unless the action is visible, approval-gated where required, resolver-backed, and auditable through Odin-owned operator surfaces.

This slice assumes the unified `odin review` queue exists. It does not add new external mutations. It makes review actions and non-job mutation paths prove the same fail-closed approval posture that job admission already has.

## Audit Summary

Audited current `origin/main`, not the dirty local checkout. Relevant artifacts:

- `CONTEXT.md`
- `docs/contracts/tui-overview.md`
- `docs/contracts/operational-autonomy.md`
- `docs/contracts/external-intake.md`
- `docs/contracts/live-driver-tools.md`
- `docs/contracts/executor-contract.md`
- `internal/app/lifecycle/review.go`
- `internal/app/lifecycle/review_sources.go`
- `internal/runtime/approvals/service.go`
- `internal/runtime/jobs/service.go`
- `internal/runtime/triggers/service.go`
- `internal/skills/policy.go`

## Existing State

Odin already has `Approval Request`, `approvals.Service`, job admission for `read_only`, `mutation`, `governance`, and `destructive` intent, trigger-carried `execution_intent`, skill invocation policy, and unified `odin review` source composition.

The remaining gap is parity across review actions and other operator action paths that do not naturally enter job admission. `odin review act` routes source-by-source, and unsupported sources such as memory proposals were visible but did not return the same receipt shape as supported or resolver-backed actions.

## Reused Components

- `odin review list/show/act`
- `odin approvals show/resolve`
- `internal/app/lifecycle/review.go`
- `internal/app/lifecycle/review_sources.go`
- `internal/runtime/approvals.Service`
- `internal/runtime/jobs.Service`
- `internal/runtime/triggers.Service`
- `internal/skills.ResolveInvocationPolicy`
- source-owned review handlers for intake, goal, approval, skill artifact, context pack, failed work, and memory proposal visibility
- SQLite approval/event persistence
- `docs/contracts/tui-overview.md`

## New Components

Add only:

- `reviewActionReceipt` JSON envelope for `odin review act --json` and unsupported mutation attempts
- `reviewActionPreflight` in `internal/app/lifecycle` to classify source/action support and mutation scope
- receipt wrappers around existing source-owned review handlers

Do not add a new queue, approval table, resolver registry, broad policy engine, runtime daemon, batch approval, or external mutation adapter.

## Why New Components Are Necessary

`odin review` is now the cross-source decision surface. A narrow receipt contract makes every review action prove source ownership, support, mutation scope, approval status, resolver support, durable mutation, audit event, refusal key, and next step without moving business logic out of existing authorities.

## Locked Domain Decisions

- `odin review` remains the unified operator decision queue.
- `Approval Request` remains the canonical approval object.
- `approvals.Service` remains the canonical resolver for Approval Requests.
- Job admission remains the canonical executable Work Item dispatch gate.
- Review actions do not gain new authority by appearing in the queue.
- Unsupported review actions fail closed and leave source state unchanged.
- External-world mutation is out of scope until a source-specific resolver contract, approval policy, and real Odin proof are added.

No ADR is required because this strengthens existing contracts without changing the primary architecture.

## Selected Design

`odin review act <queue-id> <action> --json` returns a receipt envelope with:

- `review_id`
- `queue_id`
- `source_type`
- `source_id`
- `action`
- `status`
- `result`
- `supported`
- `mutation_scope`
- `approval_required`
- `approval_status`
- `resolver_support`
- `mutated`
- `audit_event`
- `error`
- `next_step`
- `source_result`

Allowed `mutation_scope` values are `none`, `review_state`, `execution_resuming`, `external_world`, and `unsupported`. External-world mutation must fail closed in this slice unless a future source proves a supported resolver and approved `Approval Request`.

## Rejected Alternatives

Rejected: build a runtime-wide policy engine now. Existing job admission, skills policy, trigger intent, and approvals services already own core policy.

Rejected: make every review action create an `Approval Request`. Many review actions only mutate Odin review state.

Rejected: alias `odin review approve/reject` across every source as the main UX fix. Aliases do not solve parity or receipt proof.

Rejected: add real message, calendar, purchase, deploy, publish, or financial/legal/medical mutations. This slice hardens the gate before expanding capability.

## Test And Verification Plan

Focused tests:

```bash
go test ./internal/app/lifecycle -run 'TestReview.*Receipt|TestRunReview.*Unsupported|TestRunUnifiedReviewQueue' -count=1
go test ./internal/runtime/approvals ./internal/runtime/jobs ./internal/runtime/triggers ./internal/skills -run 'Approval|ExecutionIntent|Governance|Destructive|Policy' -count=1
```

Real operator proof after build:

```bash
which odin
realpath "$(which odin)"
make build
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin help
ODIN_ROOT="$tmp" ./bin/odin review list --json
ODIN_ROOT="$tmp" ./bin/odin review show <queue-id> --json
ODIN_ROOT="$tmp" ./bin/odin review act <queue-id> <supported-action> --json
ODIN_ROOT="$tmp" ./bin/odin review act <unsupported-queue-id> approve --json
```

## Documentation Changes

Update `docs/contracts/tui-overview.md` with the review-action receipt contract. Update `docs/contracts/runtime-events.md` only if a new audit event or stable refusal key is introduced.

## Security Review

Implementation must preserve no hidden resolver support, no batch approval, no external-world mutation, no approval resolution outside `approvals.Service`, no direct dispatch outside job admission, no direct live-driver mutation from review actions, and no unsupported source mutation.

## Open Blockers

None for implementation planning. The original local checkout had unrelated dirty changes, so implementation should happen in a clean worktree.

## Planning Handoff

Implement one PR-sized slice:

1. Add receipt and unsupported-action tests.
2. Add `reviewActionReceipt` and `reviewActionPreflight` in `internal/app/lifecycle`.
3. Wrap existing `review act` handlers without moving source-owned business rules.
4. Add high-risk intent category characterization.
5. Document the receipt contract.
6. Prove with focused tests and real `odin` commands.

## Implementation Goal Prompt

```text
/goal Implement Approval-Gated Execution slice 3 in /home/orchestrator/odin-os.

Use docs/superpowers/specs/2026-05-10-approval-gated-execution-policy-parity-design.md as the approved design. Keep it PR-sized and make atomic commits. Reuse odin review, odin approvals, approvals.Service, jobs admission, trigger execution_intent, skills policy, existing source-owned review handlers, and runtime events. Do not add a parallel queue, broad policy engine, resolver registry, batch approval, or any real-world external mutation.

Required local proof:
- go test ./internal/app/lifecycle -run 'TestReview.*Receipt|TestRunReview.*Unsupported|TestRunUnifiedReviewQueue' -count=1
- go test ./internal/runtime/approvals ./internal/runtime/jobs ./internal/runtime/triggers ./internal/skills -run 'Approval|ExecutionIntent|Governance|Destructive|Policy' -count=1
- make build
- which odin && realpath "$(which odin)"
- ODIN_ROOT="$(mktemp -d)" ./bin/odin help
- with that ODIN_ROOT, prove review list/show JSON, one supported review act receipt, and one unsupported review act refusal
```
