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
| prompt-to-production atomic commits/tests/review/PR | PR #219 adds dry-run/local `odin work pr prepare`, approval-backed `--live --approval <id>`, PR handoff evidence, proof readback, and a controlled GitHub API fixture test. | Partial; PR #219 open |
| approval before merge/deploy | PR #222 adds local-only `odin work approval request --kind merge\|deploy`, separate Approval Requests, approval-purpose proof readback, and fail-closed prerequisite checks before any merge/deploy mutation exists. | Partial; PR #213/#214/#219/#222 open |
| operating rule applied everywhere | Many surfaces now satisfy real command + persistence + policy + audit. Open PRs and reviewer execution gaps mean the rule is not yet universal. | Not complete |

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
selection evidence. It now also adds approval-backed
`odin work pr prepare --live --approval <id>` through the existing
`internal/review` PR handoff seam. That still deliberately keeps merge/deploy
approval outside the command.

PR #222 layers on top of PR #219 and the delivery evidence/gate work. It adds a
local-only merge/deploy approval proof surface:
`odin work approval request --task <id|key> --kind <merge|deploy>`. The command
creates separate Approval Requests for merge and deploy, reads them back through
`odin work proof --task`, and fails closed unless PR handoff, review-selection
evidence, delivery gate evidence, and merge-before-deploy ordering are present.
It intentionally does not merge, deploy, delete branches, create releases, or
call production mutation APIs.

## What Is Open But Not Yet Main

- PR #218: capability/plugin model clarification through `odin capabilities`.
- PR #216: failed-work lane in overview/TUI.
- PR #213: delivery evidence recording through `odin work`.
- PR #214: delivery gate advancement from recorded evidence.
- PR #219: prompt-to-production proof command, including pre-work intake proof
  and dry-run/local plus approval-backed live PR handoff evidence.
- PR #221: high-risk approval parity for explicit operator dispatch.
- PR #222: merge/deploy approval resolver proof through local-only Approval
  Requests and `work proof` gate readback.

## Remaining Gaps

1. Merge and deploy approval resolver proof exists in PR #222, but it is stacked
   on open draft PRs and not current `main` behavior.
2. Reviewer, QA, and security handoff rows exist, but reviewer execution is not
   yet represented as first-class Run Attempts.
3. PR #219 does not perform a real live GitHub.com write; its approved live
   mutation path is proved against a controlled HTTP fixture and fails closed
   without `GITHUB_TOKEN` in real `./bin/odin` proof.
4. Several green PRs are still drafts or unmerged, so their behavior is not
   current `main` behavior.

## Next Concrete Slice

The next non-duplicative implementation slice should be reviewer/QA/security
execution as first-class Run Attempts, not more scheduler-trigger work and not
another PR handoff or approval-request command.

Existing proof and handoff command shape from PR #219:

```text
odin work proof (--task <id|key>|--intake <id|key>) [--json]
odin work pr prepare --task <id|key> --summary <text> --tests <text> --risk <text> [--blocker <text>] [--dry-run|--live --approval <id>] [--json]
```

Required constraints:

- Preserve PR handoff as evidence only; do not treat it as merge or deploy
  approval.
- Reuse PR #213 delivery evidence and PR #214 gate advancement structures.
- Add approval records, resolver receipts, and runtime events for merge/deploy
  decisions before any external merge or deployment mutation exists.
- Keep merge and deploy as separate approvals.
- Make `odin work proof --task` read back merge/deploy approval evidence.

Implemented in PR #222:

- `odin work approval request --task <id|key> --kind <merge|deploy> [--json]`
- merge and deploy Approval Requests with `requested_by=work_merge_gate` and
  `requested_by=work_deploy_gate`
- `work proof` merge/deploy gate readback with approval IDs, approval statuses,
  and approval purpose
- fail-closed prerequisite checks for PR handoff, review-selection evidence,
  `branch_finished` gate advancement, and merge-before-deploy ordering
- no GitHub merge API, deployment system, branch deletion, release creation, or
  production mutation

## Implementation Goal Prompt

```text
/goal Design and implement merge/deploy approval resolver proof for /home/orchestrator/odin-os.

Use docs/superpowers/specs/2026-05-11-odin-operating-rule-completion-audit.md and docs/superpowers/specs/2026-05-11-prompt-to-production-proof-path-design.md as the audit/design inputs. Keep the slice PR-sized. Build on PR #219 prompt-to-production proof and PR #213/#214 delivery evidence/gate work when available. Reuse odin work proof, approvals.Service, runtime events, pull_request_handoffs, delivery evidence records, and delivery gate records. Do not add autonomous merge, autonomous deploy, branch deletion, batch approval, or a new PR runtime.

Required behavior:
- represent merge approval and deploy approval as separate Approval Requests or gate receipts
- expose merge/deploy approval state in `odin work proof --task`
- fail closed when PR handoff, required review evidence, delivery evidence, or explicit approval is missing
- preserve human merge/deploy boundaries; do not call GitHub merge APIs or deployment systems in this slice
- append runtime audit evidence for approval request, approval resolution, and gate state transitions

Required verification:
- focused lifecycle tests for merge approval required, deploy approval required, approved merge gate readback, denied gate refusal, and missing evidence refusal
- store/event tests for gate receipt persistence and audit correlation
- git diff --check
- make build
- real ./bin/odin proof on a fresh ODIN_ROOT covering intake -> accepted task -> PR handoff prepare -> merge/deploy approval required -> work proof
- make ci

Open a PR with Summary, Proven, Unproven, and Commands Run. Do not merge or deploy without explicit approval.
```
