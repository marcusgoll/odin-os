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
| prompt-to-production atomic commits/tests/review/PR | PR #219 adds dry-run/local `odin work pr prepare`, approval-backed `--live --approval <id>`, PR handoff evidence, proof readback, and a controlled GitHub API fixture test. PR #224 adds a gated operator live-smoke path for proving that same command against a disposable GitHub repository. | Partial; PR #219/#224 open |
| approval before merge/deploy | PR #222 adds local-only `odin work approval request --kind merge\|deploy`, separate Approval Requests, approval-purpose proof readback, and fail-closed prerequisite checks before any merge/deploy mutation exists. PR #223 tightens this by requiring completed selected review role Run Attempts before merge/deploy approval requests. | Partial; PR #213/#214/#219/#222/#223 open |
| operating rule applied everywhere | Many surfaces now satisfy real command + persistence + policy + audit. Open stacked draft PRs and the absence of real GitHub.com write proof mean the rule is not yet universal. | Not complete |

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

PR #223 layers on top of PR #222 and converts selected PR handoff review roles
into first-class Run Attempts. It adds
`odin work pr review run --task <id|key> --role <reviewer|qa|security>
--summary <text>`, records completed `pull_request_review:<role>` runs, links
`work proof` review-result rows back to run IDs, and changes merge/deploy
approval prerequisites from selection evidence to completed selected role run
evidence. It remains local-only and does not perform GitHub review, merge,
deploy, branch deletion, or production mutation.

PR #224 layers on top of PR #223 and adds an opt-in live PR handoff smoke path:
`scripts/ops/pr-handoff-live-smoke.sh` plus
`docs/operations/pr-handoff-live-smoke.md`. The script is gated by
`ODIN_LIVE_PR_HANDOFF_SMOKE=1`, requires a disposable GitHub repository, an
existing disposable head branch, and `GITHUB_TOKEN`, then exercises
`odin work pr prepare --live`, `odin approvals resolve`,
`odin work pr prepare --live --approval <id>`, `odin work proof`, and
`odin logs trail`. CI only runs the non-mutating contract test. No live
GitHub.com PR was created in this audit update.

PR #216 covers the failed-work observability gap without adding a second
runtime authority. It renders existing recovery-guidance data as `Failed Work`
inside `Attention` and `Observability`, including retry eligibility, retry
counts, source, last error, and recovery recommendation. Its real `./bin/odin`
proof used a fresh `ODIN_ROOT`, dispatched a fixture-backed failing Work Item,
then verified `overview` and `overview --json` showed the failed-work lane.

PR #218 covers the plugin-model naming gap through the canonical capability
gateway. It adds read-only `odin capabilities list` and
`odin capabilities show`, returns `source=capability_gateway`, and pins
`plugin_model=plugins_are_packages_not_runtime_kind`. The documented contract
keeps `agent`, `skill`, `workflow`, `command`, and `tool` as runtime capability
kinds while plugins remain packages or distribution containers, not a scheduler,
approval, executor, policy, or runtime kind.

PR #221 covers the high-risk approval parity categories through the explicit
operator dispatch path. Its lifecycle test starts read-only Work Items for
sending messages, deleting data, deployment, calendar mutation, public posting,
production changes, purchases, permission changes, financial records, legal
records, and medical records, then proves `odin work dispatch --task --json`
blocks each one with `reason=approval_required` and
`execution_intent_source=safety_classifier`. Its job-service tests also cover
the lower-level classifier and `ExecuteNextQueued` path, including durable
approval request and audit-event evidence.

## What Is Open But Not Yet Main

- PR #211: managed-project delivery profile surfaced through `odin work
  profiles` and `work status`, without creating a parallel workflow runtime.
- PR #212: capability truth overview gate that separates authored registry
  inventory from runtime-proven capability claims.
- PR #218: capability/plugin model clarification through `odin capabilities`.
- PR #216: failed-work lane in overview/TUI.
- PR #213: delivery evidence recording through `odin work`.
- PR #214: delivery gate advancement from recorded evidence.
- PR #219: prompt-to-production proof command, including pre-work intake proof
  and dry-run/local plus approval-backed live PR handoff evidence.
- PR #221: high-risk approval parity for explicit operator dispatch.
- PR #222: merge/deploy approval resolver proof through local-only Approval
  Requests and `work proof` gate readback.
- PR #223: reviewer, QA, and security role execution proof as first-class Run
  Attempts before merge/deploy approval requests.
- PR #224: opt-in live PR handoff smoke path for disposable GitHub repositories.

## Remaining Gaps

1. Merge and deploy approval resolver proof exists in PR #222, but it is stacked
   on open draft PRs and not current `main` behavior. PR #223 is also stacked
   and not current `main` behavior.
2. PR #224 provides a gated live GitHub.com PR handoff smoke path, but the
   actual live smoke has not been run against a disposable repository with an
   approved token and existing disposable head branch.
3. Several green PRs are still drafts or unmerged, so their behavior is not
   current `main` behavior.

## Completion Audit Verdict

Current `origin/main` at the time of this update was `eb51743` (`Add governed
memory proposal lifecycle (#217)`). The objective is not complete on current
`main`.

Concrete success criteria from the objective:

1. Scheduler triggers must have real `odin` create/list/show/test flows with
   event envelope, dedupe, approval preview, audit events, and next-run preview.
2. Mutating-path approval gates must be proven for messages, data deletion,
   deployment, calendar mutation, public posting, production changes, purchases,
   permissions, and financial/legal/medical records.
3. Plugin terminology must route through the capability/tool model or a thin
   first-class plugin layer over the existing gateway, with no parallel plugin
   runtime.
4. Overview/TUI must surface raw intake, review queue, triggers, approvals,
   recovery, running jobs, failed jobs, and blocked items.
5. Prompt-to-production must prove vague intake or goal through clarification,
   spec/ticket, atomic commits, tests, review, PR handoff, and approval before
   merge/deploy.
6. Each claimed automation path must be backed by a real `odin` command that
   persists state, enforces policy, and emits audit evidence.

Completion evidence status:

- Criteria 1 is implemented on `main`.
- Criteria 2 is partially implemented on `main`; exact explicit-dispatch parity
  remains in green draft PR #221.
- Criteria 3 remains in green draft PR #218.
- Criteria 4 is mostly implemented on `main`; failed-work rendering remains in
  green draft PR #216.
- Criteria 5 remains split across green draft PRs #213, #214, #219, #222, #223,
  and #224. PR #224's live GitHub smoke has not run.
- Criteria 6 is satisfied for merged trigger/review/overview pieces, but not
  universal while the proof-path, approval-parity, capabilities, failed-work,
  merge/deploy approval, and live-smoke pieces remain open.

Latest checked PR state for #211, #212, #213, #214, #216, #218, #219, #220,
#221, #222, #223, and #224 was clean with green remote checks. PR #212 is
ready-for-review rather than draft; the others are draft. That is readiness
evidence, not completion evidence.

Live proof blocker:

- `ODIN_LIVE_PR_HANDOFF_SMOKE` was unset.
- `ODIN_LIVE_PR_HANDOFF_REPO` was unset.
- `ODIN_LIVE_PR_HANDOFF_HEAD_BRANCH` was unset.
- `GITHUB_TOKEN` was unset.

Fail-closed preflight evidence:

- `scripts/ops/pr-handoff-live-smoke.sh` exited with code 2 and printed
  `set ODIN_LIVE_PR_HANDOFF_SMOKE=1 to run the live PR handoff smoke proof`.
- `ODIN_LIVE_PR_HANDOFF_SMOKE=1 scripts/ops/pr-handoff-live-smoke.sh` exited
  with code 2 and printed
  `ODIN_LIVE_PR_HANDOFF_REPO must be owner/repo for a disposable repository`.

Therefore the remaining action is an explicit operator decision, not another
local design slice: either approve and provide a disposable GitHub live-smoke
target for PR #224, or approve integration of the green draft stack.

## Operator Decision Packet

The next step must choose exactly one path.

### Option A: Run PR #224 live smoke first

Use this when the operator wants live GitHub.com proof before integrating the
stack.

Required operator-provided inputs:

- explicit approval to create or update one visible disposable GitHub PR
- disposable repository in `owner/repo` form
- existing disposable head branch in that repository
- token supplied as `GITHUB_TOKEN` with only the accepted pull-request write
  scope for that disposable repository

Allowed mutation:

- one PR create/update handoff through
  `odin work pr prepare --live --approval <id>`

Forbidden mutations:

- merge, deploy, branch deletion, release creation, repository settings
  mutation, secret mutation, public follow-up publishing, or batch approval

Required proof to capture:

- disposable repo and branch, without token value
- Approval Request ID and approval resolution event
- PR URL and number from `work proof`
- `logs trail` evidence for approval and `pull_request.handoff_prepared`
- explicit `prs=not_merged` and `deploy=not_started` proof

Stop if:

- the target repository or branch is not disposable
- `GITHUB_TOKEN`, `ODIN_LIVE_PR_HANDOFF_REPO`, or
  `ODIN_LIVE_PR_HANDOFF_HEAD_BRANCH` is missing
- the token scope is broader than the operator accepted
- the command would mutate anything beyond the single PR handoff

### Option B: Integrate the green draft stack first

Use this when the operator accepts the existing local, fixture, and CI proof as
enough to start making the open behavior current `main`.

Recommended order:

1. Merge #212 if accepted, because it is already ready-for-review and separates
   authored assets from runtime-proven capability claims.
2. Merge or retarget #213 and #214 as the delivery evidence/gate base.
3. Merge #216, #218, and #221 as independent main-based objective closures.
4. Merge or retarget #219, preserving its dependency on delivery evidence when
   needed.
5. Refresh #222, #223, and #224 after their bases are current, then decide
   whether PR #224 live smoke is still required before merge.

Integration stop conditions:

- any PR body loses `## Summary`, `## Proven`, `## Unproven`, or
  `## Commands Run`
- any remote check fails
- a rebase changes the real `odin` proof path or weakens approval policy
- merge/deploy/live GitHub mutation appears outside the approved PR handoff
  smoke path

## Draft Stack Readiness

Current non-mutating stack-readiness check:

| PR | Branch | Base | Remote checks | PR body contract |
| --- | --- | --- | --- | --- |
| #211 | `codex/approval-gates-policy-parity-current` | `main` | GitGuardian, two `go`, and `odin-e2e` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |
| #212 | `codex/risk-hardening-design` | `main` | GitGuardian, two `go`, and `odin-e2e` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |
| #213 | `codex/work-evidence-current` | `main` | GitGuardian, two `go`, and `odin-e2e` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |
| #214 | `codex/work-advance-current` | `codex/work-evidence-current` | GitGuardian and two `go` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |
| #216 | `codex/overview-failed-work-lane-current` | `main` | GitGuardian, two `go`, and `odin-e2e` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |
| #218 | `codex/capabilities-operator-cli-current` | `main` | GitGuardian, two `go`, and `odin-e2e` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |
| #219 | `codex/prompt-to-production-proof-design` | `main` | GitGuardian, two `go`, and `odin-e2e` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |
| #221 | `codex/high-risk-approval-parity` | `main` | GitGuardian, two `go`, and `odin-e2e` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |
| #222 | `codex/merge-deploy-approval-proof` | `main` | GitGuardian, two `go`, and `odin-e2e` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |
| #223 | `codex/review-run-attempt-proof` | `codex/merge-deploy-approval-proof` | GitGuardian and two `go` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |
| #224 | `codex/pr-handoff-live-smoke` | `codex/review-run-attempt-proof` | GitGuardian and two `go` passing | Has `## Summary`, `## Proven`, `## Unproven`, `## Commands Run` |

The stack is ready for an operator decision, but not merged. The important
integration caveat is that PR #222 currently has `main` as its GitHub base while
its branch includes PR #213/#214 delivery-gate work and PR #219 prompt-to-
production proof work. A merge of #222 would carry those prerequisite commits
with it. Preserve this relationship when deciding whether to retarget, merge
bottom-up, or close superseded draft PRs.

## Next Concrete Slice

The next non-duplicative implementation slice should be either running the PR
#224 live smoke with explicit operator approval against a disposable GitHub
repository, or draft-stack integration. It should not be more scheduler-trigger
work and not another local-only PR handoff, review-run, or approval-request
command.

If live proof is blocked, the next operator integration action is to choose one
of these options:

- merge bottom-up: #213, #214, #219, then rebase/refresh #222/#223/#224
- merge the aggregate #222 branch after accepting that it carries #213/#214/#219
  prerequisite commits, then refresh #223/#224
- keep all PRs draft and run the PR #224 live smoke first

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

Implemented in PR #223:

- `odin work pr review run --task <id|key> --role <reviewer|qa|security>
  --summary <text> [--json]`
- selected reviewer, QA, and security roles recorded as completed
  `pull_request_review:<role>` Run Attempts
- `work proof` review-result rows linked to role run IDs
- merge/deploy approval requests fail closed until every selected review role
  has completed review-result evidence and a completed review Run Attempt
- no GitHub review API, merge API, deployment system, branch deletion, release
  creation, or production mutation

Implemented in PR #224:

- `scripts/ops/pr-handoff-live-smoke.sh`
- `docs/operations/pr-handoff-live-smoke.md`
- `scripts/tests/pr-handoff-live-smoke-test.sh`
- Makefile `ci` coverage for the non-mutating smoke contract test
- fail-closed defaults unless `ODIN_LIVE_PR_HANDOFF_SMOKE=1`,
  `ODIN_LIVE_PR_HANDOFF_REPO`, `ODIN_LIVE_PR_HANDOFF_HEAD_BRANCH`, and
  `GITHUB_TOKEN` are provided
- no live GitHub.com write during normal CI or local verification

## Implementation Goal Prompt

```text
/goal Run or integrate real live GitHub.com PR handoff proof for /home/orchestrator/odin-os.

Use docs/superpowers/specs/2026-05-11-odin-operating-rule-completion-audit.md, docs/superpowers/specs/2026-05-11-prompt-to-production-proof-path-design.md, and docs/operations/pr-handoff-live-smoke.md as the audit/design inputs. Keep the slice PR-sized. Build on PR #219 prompt-to-production proof, PR #222 merge/deploy approval proof, PR #223 review role run proof, PR #224 PR handoff live smoke, and PR #213/#214 delivery evidence/gate work when available. Reuse odin work proof, approvals.Service, runtime events, pull_request_handoffs, pull_request_review_results, runs, delivery evidence records, and delivery gate records. Do not add autonomous merge, autonomous deploy, branch deletion, batch approval, or a new PR runtime.

Required behavior:
- either run PR #224's `scripts/ops/pr-handoff-live-smoke.sh` against an explicitly operator-approved disposable GitHub repository, or integrate the stacked draft PRs if live proof is still blocked
- preserve human merge/deploy boundaries; do not call GitHub merge APIs, deployment systems, branch deletion, release creation, or repository settings APIs
- keep live PR handoff scoped to creating/updating the PR handoff artifact and durable Odin evidence
- record or verify the real GitHub PR URL/number in `pull_request_handoffs` and `work proof`
- append runtime audit evidence for approval request, approval resolution, and handoff preparation
- document any required operator token/repository setup and refusal behavior when credentials are absent

Required verification:
- if running live smoke: capture exact disposable repo, branch, approval ID, PR URL/number, `work proof`, and `logs trail` evidence without exposing tokens
- if integrating stack: verify every stacked PR body has Summary, Proven, Unproven, and Commands Run and every remote check is green
- git diff --check
- make build
- real ./bin/odin or `scripts/ops/pr-handoff-live-smoke.sh` proof on a fresh ODIN_ROOT covering Work Item -> approval request/resolution -> live PR handoff -> work proof readback, unless live proof remains blocked pending operator approval
- make ci

Open a PR with Summary, Proven, Unproven, and Commands Run. Do not merge or deploy without explicit approval.
```
