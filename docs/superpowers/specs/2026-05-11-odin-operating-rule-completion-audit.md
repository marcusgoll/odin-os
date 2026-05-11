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
- `internal/app/lifecycle/run.go`
- `internal/app/lifecycle/run_test.go`
- `internal/cli/commands/work.go`
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
| approval gate for sending messages | Main classifies "Send message to customer" as governance mutation. PR #221 proves explicit `odin work dispatch --task` reclassifies read-only operator tasks before approval blocking. | Partial on main; operator-path parity in PR #221 |
| approval gate for deleting data | Main classifies destructive deletion. PR #221 includes deleting data in operator-path parity coverage. | Partial on main; operator-path parity in PR #221 |
| approval gate for deployment | Main classifies deploy-to-production as governance mutation. PR #221 includes deployment in operator-path parity coverage. | Partial on main; operator-path parity in PR #221 |
| approval gate for calendar mutation | Main classifies changing a calendar event. PR #221 includes calendar mutation in operator-path parity coverage. | Partial on main; operator-path parity in PR #221 |
| approval gate for public posting | Main classifies publishing public content. PR #221 includes public posting in operator-path parity coverage. | Partial on main; operator-path parity in PR #221 |
| approval gate for production changes | Main classifies production system changes. PR #221 includes production changes in operator-path parity coverage. | Partial on main; operator-path parity in PR #221 |
| approval gate for purchases | PR #221 adds explicit operator-path approval parity coverage for purchases. | Open draft PR #221 |
| approval gate for permission changes | PR #221 adds explicit operator-path approval parity coverage for permission changes. | Open draft PR #221 |
| approval gate for financial records | PR #221 adds explicit operator-path approval parity coverage for financial records. | Open draft PR #221 |
| approval gate for legal records | PR #221 adds explicit operator-path approval parity coverage for legal records. | Open draft PR #221 |
| approval gate for medical records | PR #221 adds explicit operator-path approval parity coverage for medical records. | Open draft PR #221 |
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
| prompt-to-production spec/ticket | Intake processing and review-required draft artifacts exist; PR #219 proves them through `odin work proof`. | Open draft PR #219 |
| prompt-to-production atomic commits/tests/review/PR | PR #219 adds dry-run/local `odin work pr prepare` evidence and proof readback. Live GitHub PR creation/update remains intentionally unwired without an approval-backed resolver. | Partial; PR #219 open |
| approval before merge/deploy | PR body templates and proof gates preserve human merge/deploy boundaries; delivery evidence and advance gates are in PR #213 and PR #214. | Partial; PR #213/#214 open |
| operating rule applied everywhere | Many surfaces now satisfy real command + persistence + policy + audit. Open PRs and missing live PR handoff mean the rule is not yet universal. | Not complete |

## What Is Already Done

Scheduler trigger workflow is not the next implementation gap. PR #169 and PR
#210 already promoted trigger creation, inspection, test preview, event envelope,
dedupe, approval preview, and next-run evidence into operator-facing `odin`
surfaces.

Approval-gate category recognition is materially present on main. PR #221
closes a more concrete parity gap: explicit `odin work dispatch --task` for a
read-only high-risk task now persists the safety-classified intent before
approval blocking, so operator-path evidence agrees with the approval queue and
runtime log. That PR is still open, so current `main` should be described as
category-aware but not fully operator-parity complete.

PR #219 also moved prompt-to-production forward after this audit was first
written. It adds read-only `odin work proof` for intake/task evidence and
dry-run/local `odin work pr prepare` for persisted PR handoff and review
selection evidence. That is not live GitHub mutation, and it deliberately keeps
merge/deploy approval outside the command.

## What Is Open But Not Yet Main

- PR #218: capability/plugin model clarification through `odin capabilities`.
- PR #216: failed-work lane in overview/TUI.
- PR #213: delivery evidence recording through `odin work`.
- PR #214: delivery gate advancement from recorded evidence.
- PR #219: prompt-to-production proof command, including pre-work intake proof
  and dry-run/local PR handoff evidence.
- PR #221: high-risk approval parity for explicit operator dispatch.

## Remaining Gaps

1. Live PR creation/update is still missing from the prompt-to-production path.
   PR #219 proves dry-run/local handoff evidence, but `--live` fails closed
   until an approved resolver-backed GitHub mutation path exists.
2. Merge and deploy approvals remain human boundaries, but end-to-end resolver
   proof is not complete.
3. Reviewer, QA, and security handoff rows exist, but reviewer execution is not
   yet represented as first-class Run Attempts.
4. Several green PRs are still drafts or unmerged, so their behavior is not
   current `main` behavior.

## Next Concrete Slice

The next non-duplicative implementation slice should be an approval-backed live
PR handoff resolver for the existing `odin work pr prepare --live` path, not
more scheduler-trigger work and not another local-only PR evidence command.

Existing local command shape from PR #219:

```text
odin work pr prepare --task <id|key> --summary <text> --tests <text> --risk <text> [--blocker <text>] [--dry-run] [--json]
```

Required constraints:

- Preserve the dry-run/local proof default from PR #219.
- Reuse `internal/review.BuildPullRequestBody`,
  `internal/review.HandoffOrchestrator`, `PullRequestManager`, existing
  `pull_request_handoffs`, and `pull_request_review_results`.
- Never merge, deploy, delete branches, resolve approvals, or treat PR handoff
  as merge approval.
- For live GitHub upsert, require an approved resolver-backed Approval Request,
  persist mutation receipt evidence, emit an audit event, and prove token
  redaction.
- Make `odin work proof --task` read back the resulting handoff evidence.

## Implementation Goal Prompt

```text
/goal Design and implement the approval-backed live PR handoff resolver for /home/orchestrator/odin-os.

Use docs/superpowers/specs/2026-05-11-odin-operating-rule-completion-audit.md and docs/superpowers/specs/2026-05-11-prompt-to-production-proof-path-design.md as the audit/design inputs. Keep the slice PR-sized. Build on the existing odin work pr prepare dry-run/local path from PR #219. Reuse internal/review.BuildPullRequestBody, HandoffOrchestrator, PullRequestManager, pull_request_handoffs, pull_request_review_results, odin work proof, approvals.Service, and runtime events. Do not add merge, deploy, branch deletion, batch approval, or a new PR runtime.

Required behavior:
- preserve dry-run/local PR handoff behavior
- make --live require an approved Approval Request before external GitHub mutation
- use the existing PullRequestManager/HandoffOrchestrator instead of direct gh calls in the lifecycle layer
- persist mutation receipt evidence and a runtime audit event
- fail closed for missing task, missing evidence, unsupported live mutation, unapproved external mutation, token exposure risk, or GitHub API failure
- make existing work proof read back the local and live handoff evidence

Required verification:
- focused lifecycle tests for approved live handoff, unapproved live refusal, missing evidence, and GitHub API failure
- review package tests for body evidence, mutation receipt, and token redaction boundaries
- git diff --check
- make build
- real ./bin/odin proof on a fresh ODIN_ROOT covering intake -> accepted task -> dry-run PR handoff prepare -> unapproved --live refusal -> work proof
- make ci

Open a PR with Summary, Proven, Unproven, and Commands Run. Do not merge or deploy without explicit approval.
```
