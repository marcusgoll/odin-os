---
title: Odin Operating Rule Completion Audit
date: 2026-05-11
status: audit-current
scope: odin-os prompt-to-production, trigger workflow, approval gates, plugin model, observability
---

# Odin Operating Rule Completion Audit

## Objective Restatement

Odin should expose real operator-owned automation surfaces for scheduler
triggers, approval-gated execution, capability/plugin discovery, observability,
and prompt-to-production proof. A feature only counts when a real `odin` command
invokes it, persists durable state, enforces policy, and emits audit evidence.

## Audit Basis

This audit inspected `origin/main` plus current open PR state on May 11, 2026.
Open PRs are treated as useful evidence, not shipped behavior. A green draft PR
does not count as complete until it is merged or explicitly accepted as the
current artifact.

Evidence checked:

- `gh pr list --state merged` for scheduler, review queue, and trigger preview
  slices.
- `gh pr list --state open` for capability, observability, delivery, and proof
  slices.
- `internal/cli/commands/trigger.go`
- `internal/runtime/triggers/service.go`
- `internal/store/sqlite/store.go`
- `internal/runtime/jobs/service.go`
- `internal/runtime/jobs/service_test.go`
- `internal/app/lifecycle/review.go`
- `internal/app/lifecycle/run_test.go`
- `internal/cli/overview/service.go`
- `internal/cli/render/overview.go`
- `docs/superpowers/specs/2026-05-11-prompt-to-production-proof-path-design.md`

## Prompt-To-Artifact Checklist

| Requirement | Evidence | Current Status |
| --- | --- | --- |
| `trigger` create/list/show/test operator flows | PR #169 merged. `internal/cli/commands/trigger.go` contains create, list, show, fire, and test paths. | Implemented on main |
| trigger event envelope | PR #169 and PR #210 merged. `runtimeevents.AutomationTriggerEnvelope` is persisted by trigger fire/test/defer/error events. | Implemented on main |
| trigger dedupe key | `FireAutomationTrigger` materialization key prevents duplicate Work Item creation for the same trigger/source. | Implemented on main |
| trigger approval rules | Trigger execution intent and approval-required preview are surfaced through trigger test and scheduler tick views. | Implemented on main |
| trigger audit event | `automation_trigger.created`, `fire_requested`, `evaluated`, `materialized`, `tested`, and related events are emitted from store/runtime paths. | Implemented on main |
| trigger next-run preview | `trigger show` and trigger detail views expose next-run timing/readiness details. | Implemented on main |
| approval gate for sending messages | `TestClassifyTaskExecutionIntentCoversHighRiskRealWorldMutationCategories` covers "Send message to customer" as governance mutation. | Implemented on main |
| approval gate for deleting data | Same test covers destructive deletion. | Implemented on main |
| approval gate for deployment | Same test covers deploy-to-production as governance mutation. | Implemented on main |
| approval gate for calendar mutation | Same test covers changing a calendar event. | Implemented on main |
| approval gate for public posting | Same test covers publishing public content. | Implemented on main |
| approval gate for production changes | Same test covers production system changes. | Implemented on main |
| every review mutation path returns policy/receipt evidence | `reviewActionReceipt` and `reviewActionPreflight` exist in `internal/app/lifecycle/review.go`; review-action tests assert receipt/refusal behavior. | Implemented on main |
| plugin model clarified | PR #218 adds `odin capabilities list/show` and documents plugins as packages, not a runtime kind. | Open draft PR #218 |
| overview/TUI raw intake | `internal/cli/overview/service.go` has raw intake lane/projection code. | Implemented on main |
| overview/TUI review queue | Governed review queue surfaces are present in overview/review code after merged review queue work. | Implemented on main |
| overview/TUI triggers | Automation trigger lane is implemented in overview service/rendering. | Implemented on main |
| overview/TUI approvals | Approval counts and resolver visibility are present in overview/review surfaces. | Implemented on main |
| overview/TUI recovery | Recovery and failed-work guidance exist in projections; failed-work rendering is still a separate PR. | Partial; PR #216 open |
| overview/TUI running jobs | Active execution and run attempts are present in overview. | Implemented on main |
| overview/TUI failed jobs | PR #216 renders `Failed Work` from existing recovery-guidance data. | Open draft PR #216 |
| overview/TUI blocked items | Attention/blocked work surfaces exist in overview. | Implemented on main |
| prompt-to-production vague input clarification | PR #219 adds `odin work proof --intake` for `needs_clarification` intake before Work Item creation. | Open draft PR #219 |
| prompt-to-production spec/ticket | Intake processing and review-required draft artifacts exist; PR #219 proves them. | Open draft PR #219 |
| prompt-to-production atomic commits/tests/review/PR | PR handoff storage and review selection exist, but no approved operator command wires live PR creation/update into the work lifecycle. | Missing live operator command |
| approval before merge/deploy | PR body templates and proof gates preserve human merge/deploy boundaries; delivery evidence and advance gates are in PR #213 and PR #214. | Partial; PR #213/#214 open |
| operating rule applied everywhere | Many surfaces now satisfy real command + persistence + policy + audit. Open PRs and missing live PR handoff mean the rule is not yet universal. | Not complete |

## What Is Already Done

Scheduler trigger workflow is not the next implementation gap. PR #169 and PR
#210 already promoted trigger creation, inspection, test preview, event envelope,
dedupe, approval preview, and next-run evidence into operator-facing `odin`
surfaces.

Approval-gate category coverage is also materially present on main. The
remaining approval risk is not category recognition; it is keeping future
external mutation resolvers from bypassing existing job admission, review
receipt, approval, and audit contracts.

## What Is Open But Not Yet Main

- PR #218: capability/plugin model clarification through `odin capabilities`.
- PR #216: failed-work lane in overview/TUI.
- PR #213: delivery evidence recording through `odin work`.
- PR #214: delivery gate advancement from recorded evidence.
- PR #219: prompt-to-production proof command, including pre-work intake proof.

## Remaining Gaps

1. Live PR creation/update is still missing from the prompt-to-production path.
   `internal/review.GitHubPullRequestManager` and `HandoffOrchestrator` exist,
   but there is no approved `odin` command that gates, persists, and audits a
   PR handoff mutation.
2. Merge and deploy approvals remain human boundaries, but end-to-end resolver
   proof is not complete.
3. Reviewer, QA, and security handoff rows exist, but reviewer execution is not
   yet represented as first-class Run Attempts.
4. Several green PRs are still drafts or unmerged, so their behavior is not yet
   current `main` behavior.

## Next Concrete Slice

The next non-duplicative implementation slice should be an approval-gated PR
handoff command, not more scheduler-trigger work.

Proposed command shape:

```text
odin work pr prepare --task <id|key> --summary <text> --tests <text> --risk <text> [--blocker <text>] [--dry-run] [--json]
```

Required constraints:

- Default to dry-run or local-only proof until an Approval Request authorizes
  external GitHub mutation.
- Reuse `internal/review.BuildPullRequestBody`,
  `internal/review.HandoffOrchestrator`, `PullRequestManager`, existing
  `pull_request_handoffs`, and `pull_request_review_results`.
- Never merge, deploy, delete branches, resolve approvals, or treat PR handoff
  as merge approval.
- Persist a handoff record and runtime event for local/dry-run prepare.
- For live GitHub upsert, require an approved resolver-backed Approval Request
  and prove token redaction.
- Make `odin work proof --task` read back the resulting handoff evidence.

## Implementation Goal Prompt

```text
/goal Design and implement an approval-gated PR handoff command in /home/orchestrator/odin-os.

Use docs/superpowers/specs/2026-05-11-odin-operating-rule-completion-audit.md and docs/superpowers/specs/2026-05-11-prompt-to-production-proof-path-design.md as the audit/design inputs. Keep the slice PR-sized. Reuse internal/review.BuildPullRequestBody, HandoffOrchestrator, PullRequestManager, pull_request_handoffs, pull_request_review_results, odin work proof, approvals.Service, and runtime events. Do not add merge, deploy, branch deletion, batch approval, or a new PR runtime.

Required behavior:
- add an operator command for preparing PR handoff evidence from a Work Item
- default to dry-run/local proof unless external GitHub mutation has explicit approval
- persist handoff/review-selection evidence and an audit event
- fail closed for missing task, missing evidence, unsupported live mutation, or unapproved external mutation
- make existing work proof read back the handoff evidence

Required verification:
- focused lifecycle tests for dry-run/local handoff, missing evidence, and unapproved live mutation refusal
- review package tests for body evidence and token redaction boundaries
- git diff --check
- make build
- real ./bin/odin proof on a fresh ODIN_ROOT covering intake -> accepted task -> PR handoff prepare -> work proof
- make ci

Open a PR with Summary, Proven, Unproven, and Commands Run. Do not merge or deploy without explicit approval.
```

