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
| trigger dedupe key | Current-main `./bin/odin trigger fire` proof shows repeated manual fire with the same materialization key returns `created_work_item=false` and leaves one queued automation Work Item. | Implemented on main |
| trigger approval rules | Trigger execution intent and approval-required preview are surfaced through trigger test and scheduler tick views; current-main governance `trigger fire` proof materializes a blocked Work Item with a pending governance Approval Request. | Implemented on main |
| trigger audit event | `automation_trigger.created`, `fire_requested`, `evaluated`, `materialized`, `tested`, and related events are emitted from store/runtime paths. | Implemented on main |
| trigger next-run preview | `trigger show` and trigger detail views expose next-run timing/readiness details. | Implemented on main |
| approval gate for sending messages | Current-main `./bin/odin work dispatch --task --json` blocks this read-only operator task with `approval_required`; PR #221 persists the safety-classified governance intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
| approval gate for deleting data | Current-main matrix blocks this read-only operator task with `approval_required`; PR #221 persists destructive safety-classified intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
| approval gate for deployment | Current-main matrix blocks this read-only operator task with `approval_required`; PR #221 persists safety-classified governance intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
| approval gate for calendar mutation | Current-main matrix blocks this read-only operator task with `approval_required`; PR #221 persists safety-classified governance intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
| approval gate for public posting | Current-main matrix blocks this read-only operator task with `approval_required`; PR #221 persists safety-classified governance intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
| approval gate for production changes | Current-main matrix blocks this read-only operator task with `approval_required`; PR #221 persists safety-classified governance intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
| approval gate for purchases | Current-main matrix blocks this read-only operator task with `approval_required`; PR #221 persists safety-classified governance intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
| approval gate for permission changes | Current-main matrix blocks this read-only operator task with `approval_required`; PR #221 persists safety-classified governance intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
| approval gate for financial records | Current-main matrix blocks this read-only operator task with `approval_required`; PR #221 persists safety-classified governance intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
| approval gate for legal records | Current-main matrix blocks this read-only operator task with `approval_required`; PR #221 persists safety-classified governance intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
| approval gate for medical records | Current-main matrix blocks this read-only operator task with `approval_required`; PR #221 persists safety-classified governance intent before blocking. | Partial on main; operator-path intent parity in PR #221 |
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

## Traceability Matrix

This matrix is the final-completion checklist. A row does not count complete
until the named proof is on current `main` or the operator explicitly accepts
the open PR proof as the target artifact.

| Objective deliverable | Required proof before completion | Current evidence holder |
| --- | --- | --- |
| Trigger create/list/show/test flows | `odin trigger create`, `odin trigger list`, `odin trigger show`, and `odin trigger test` against a fresh `ODIN_ROOT`, plus persisted trigger and event readback | `main` after PR #169/#210 |
| Trigger event envelope, dedupe, approval rules, audit events, next-run preview | Trigger tests and real command output showing envelope fields, materialization key or dedupe behavior, approval-required preview, `automation_trigger.*` events, and next due/run timing | `main` after PR #169/#210 |
| High-risk approval gates for messages, deletion, deployment, calendar mutation, public posting, production changes, purchases, permissions, and financial/legal/medical records | `odin work start --intent read_only` plus `odin work dispatch --task --json` blocks each category with `reason=approval_required` and `execution_intent_source=safety_classifier`; `odin approvals all --json` and `odin logs --json` show pending approvals and audit events | PR #221 |
| Review mutations preserve policy and receipt evidence | `odin review act ... --json` returns source-owned receipt/refusal fields and fails closed for unsupported/high-risk actions | `main` |
| Plugin model clarified without parallel plugin runtime | `odin capabilities list --kind command --json` and `odin capabilities show project.status --json` report `source=capability_gateway` and `plugin_model=plugins_are_packages_not_runtime_kind`; docs state plugins are packages, not runtime kinds | PR #218 |
| Overview/TUI raw intake, review queue, triggers, approvals, recovery, running work, failed work, blocked work | `odin overview` and `odin overview --json` show all required lanes, including `Failed Work` from recovery guidance | mostly `main`; failed-work rendering in PR #216 |
| Vague issue or goal produces clarification and spec/ticket evidence | `odin work proof --intake <id|key> --json` shows `needs_clarification`, draft task/spec/ticket evidence, or no Work Item created until review conditions are met | PR #219 |
| Prompt-to-production PR handoff with tests/review evidence | `odin work pr prepare --task ... --dry-run --json`, `odin work proof --task ... --json`, and controlled fixture/live proof show summary, tests, risk, review-role selection, PR URL/number when live, and audit events | PR #219 and PR #224 |
| Reviewer, QA, and security review are Run Attempts before merge/deploy approval | `odin work pr review run --role reviewer|qa|security --json` records completed `pull_request_review:<role>` runs and `work proof` links review results to run IDs | PR #223 |
| Approval before merge/deploy | `odin work approval request --kind merge` and `--kind deploy` create separate Approval Requests only after PR handoff, review Run Attempts, delivery evidence, and delivery gates are present; `work proof` reads back purpose/status and deploy fails closed until merge approval exists | PR #222 plus PR #223 |
| Operating rule everywhere | For every claimed workflow, real `odin` command proof invokes the path, persists state, enforces policy, and emits audit evidence; green CI alone is insufficient | not complete until the open stack is merged or explicitly accepted |

## Real Odin Trigger Proof

Fresh proof on the doc-only audit branch after `make build`:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" HOME=$(mktemp -d) ./bin/odin project select odin-core
ODIN_ROOT="$tmp" HOME=$(mktemp -d) ./bin/odin trigger create operator-daily initiative=odin-core kind=schedule status=enabled cadence=1h next=2026-05-05T03:00:00Z title=Operator_daily_review summary=operator_review quiet=02:00-06:00 batch=ops-review batch_window=1h intent=governance --json
ODIN_ROOT="$tmp" HOME=$(mktemp -d) ./bin/odin trigger list --json
ODIN_ROOT="$tmp" HOME=$(mktemp -d) ./bin/odin trigger show operator-daily
ODIN_ROOT="$tmp" HOME=$(mktemp -d) ./bin/odin trigger test operator-daily now=2026-05-05T03:30:00Z --json
ODIN_ROOT="$tmp" HOME=$(mktemp -d) ./bin/odin jobs --json
ODIN_ROOT="$tmp" HOME=$(mktemp -d) ./bin/odin trigger audit operator-daily --json
rm -rf "$tmp"
```

Observed proof:

- `trigger create` persisted `operator-daily` with `status=enabled`,
  `kind=schedule`, `next_eligible_at=2026-05-05T03:00:00Z`, and
  `execution_intent=governance` inside `rule_json`.
- `trigger list --json` read back the persisted trigger.
- `trigger show operator-daily` showed `next_run=2026-05-05T03:00:00Z`,
  `quiet_hours=02:00-06:00`, `batch=ops-review window=1h`,
  `approval_required=true`, `last_materialization=none`, and
  `audit_events=1`.
- `trigger test ... --json` returned `dry_run=true`, `mutates=false`,
  `decision=defer`, `reason=quiet_hours`, `approval_required=true`, and an
  `event_envelope` containing `source=schedule`, `trigger_type=schedule`,
  `dedupe_key=default:operator-daily:schedule:quiet_hours`,
  `due_at=2026-05-05T03:00:00Z`, and risk
  `execution_intent=governance` / `approval_required=true`.
- `jobs --json` returned an empty job list after `trigger test`, proving the
  test path did not materialize work.
- `trigger audit --json` returned `automation_trigger.created` and
  `automation_trigger.tested` events, with the tested event carrying the same
  approval-required envelope evidence.

Fresh current-main dedupe/materialization proof on the same doc-only branch:

```bash
make build
tmp=$(mktemp -d)
home=$(mktemp -d)
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin project select odin-core
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin transition set cutover confirm because audit-trigger-dedupe
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin trigger create dedupe-daily initiative=odin-core kind=schedule status=enabled cadence=1h next=2026-05-05T03:00:00Z title=Dedupe_daily summary=dedupe_review intent=read_only --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin trigger fire dedupe-daily reason=audit-dedupe --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin trigger fire dedupe-daily reason=audit-dedupe --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin jobs --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin trigger audit dedupe-daily --json
rm -rf "$tmp" "$home"
```

Observed proof:

- First `trigger fire` returned
  `materialization_key=default:dedupe-daily:manual:audit-dedupe`,
  `created_work_item=true`, and a queued automation Work Item with
  `requested_by=automation_trigger:dedupe-daily`,
  `work_kind=automation_trigger`,
  `execution_intent=read_only`, and `execution_intent_source=trigger`.
- Second `trigger fire` with the same reason returned the same
  materialization key and Work Item, but `created_work_item=false`.
- `jobs --json` showed exactly one queued automation-trigger Work Item.
- `trigger audit --json` showed two `automation_trigger.fire_requested` events,
  two `automation_trigger.evaluated` events, one
  `automation_trigger.materialized` event, and the repeated envelope
  `dedupe_key=default:dedupe-daily:manual:audit-dedupe`.

Fresh governance-trigger approval proof on the same current-main-equivalent
branch:

```bash
make build
tmp=$(mktemp -d)
home=$(mktemp -d)
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin project select odin-core
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin transition set cutover confirm because audit-trigger-governance-approval
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin trigger create governance-daily initiative=odin-core kind=schedule status=enabled cadence=1h next=2026-05-05T03:00:00Z title=Governance_daily summary=governance_review intent=governance --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin trigger fire governance-daily reason=audit-governance --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin work dispatch --task <materialized-task-key> --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin approvals all --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin logs --json
rm -rf "$tmp" "$home"
```

Observed proof:

- `trigger fire` materialized a blocked automation Work Item, not a queued one.
- The materialized Work Item persisted `execution_intent=governance` and
  `execution_intent_source=trigger`.
- `work dispatch --task ... --json` returned `dispatched=false`,
  `reason=task_not_queued`, and a task with `blocked_reason=approval_required`.
- `approvals all --json` showed one pending approval with `risk=governance`,
  `reason=approval_required`, resolver support, and approve/deny actions.
- `logs --json` showed one `automation_trigger.materialized`, one
  `approval.requested`, and one `task.queue_state_changed` event carrying
  `execution_intent=governance`,
  `execution_intent_source=trigger`, and
  `blocked_reason=approval_required`.

## Current Main Gap Proof

Fresh proof on the doc-only audit branch after `make build`:

```bash
./bin/odin capabilities list --json
```

Observed result:

- exited non-zero with `unknown command: capabilities`
- confirms the plugin/capability terminology closure remains in PR #218, not
  current `main`

Fresh high-risk dispatch proof on the doc-only audit branch:

```bash
tmp=$(mktemp -d)
home=$(mktemp -d)
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin project select odin-core
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin transition set cutover confirm because audit-high-risk-current-main
start_output=$(ODIN_ROOT="$tmp" HOME="$home" ./bin/odin work start --project odin-core --title Send_message_to_customer --intent read_only)
task_key=$(printf '%s\n' "$start_output" | tr ' ' '\n' | sed -n 's/^key=//p')
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin work dispatch --task "$task_key" --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin approvals all --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin logs --json
rm -rf "$tmp" "$home"
```

Observed result:

- `work dispatch --task --json` returned `dispatched=false`,
  `reason=approval_required`, and `status=blocked`.
- `approvals all --json` returned a pending approval with
  `risk=governance`, `resolver_support=supported`, and approve/deny actions.
- `logs --json` returned `approval.requested`, `task.status_changed`,
  `task.queue_state_changed`, and context packet events.
- The blocked task still reported `execution_intent=read_only` and
  `execution_intent_source=operator`; it did not persist
  `execution_intent_source=safety_classifier`.

Fresh high-risk category matrix proof on the doc-only audit branch, whose code
differs from `origin/main` only by this audit document:

```bash
make build
# fresh ODIN_ROOT/HOME
./bin/odin project select odin-core
./bin/odin transition set cutover confirm because audit-high-risk-category-matrix
# for each category below:
./bin/odin work start --project odin-core --title <category> --intent read_only
./bin/odin work dispatch --task <key> --json
./bin/odin approvals all --json
./bin/odin logs --json
```

Categories proven on current-main code:

| Category | Result | Persisted intent |
| --- | --- | --- |
| Send message to customer | blocked with `approval_required` | `read_only` / `operator` |
| Delete data from customer records | blocked with `approval_required` | `read_only` / `operator` |
| Deploy code to production | blocked with `approval_required` | `read_only` / `operator` |
| Change calendar event with client | blocked with `approval_required` | `read_only` / `operator` |
| Publish public launch post | blocked with `approval_required` | `read_only` / `operator` |
| Modify production system config | blocked with `approval_required` | `read_only` / `operator` |
| Make purchase of subscription | blocked with `approval_required` | `read_only` / `operator` |
| Change permissions for repository | blocked with `approval_required` | `read_only` / `operator` |
| Update financial record | blocked with `approval_required` | `read_only` / `operator` |
| Update legal records | blocked with `approval_required` | `read_only` / `operator` |
| Update medical record | blocked with `approval_required` | `read_only` / `operator` |

`odin approvals all --json` showed 11 pending approvals and `odin logs --json`
showed 11 `approval.requested` events. All 11
`task.queue_state_changed` events kept `execution_intent_source=operator`; none
used `execution_intent_source=safety_classifier`.

This proves current `main` is category-aware enough to block every named
high-risk category in this audit. PR #221 remains necessary for operator-path
parity because it persists the safety-classified governance/destructive intent
before approval blocking, so durable task state, approval evidence, and runtime
events agree on why the work was blocked.

Fresh prompt-to-production command-surface proof on the doc-only audit branch,
whose code differs from `origin/main` only by this audit document:

```bash
make build
./bin/odin work proof --help
./bin/odin work pr prepare --help
./bin/odin work approval request --help
./bin/odin work pr review run --help
```

Observed result:

- `which odin` resolved to `/home/orchestrator/.local/bin/odin`, so this proof
  deliberately used the freshly built repo-local `./bin/odin`.
- `work proof` exited with `unknown work command: proof`.
- `work pr prepare` exited with `unknown work command: pr`.
- `work approval request` exited with `unknown work command: approval`.
- `work pr review run` exited with `unknown work command: pr`.

This directly proves the prompt-to-production proof path, PR handoff, review
Run Attempt, and merge/deploy approval-request commands remain open-PR behavior
in #219/#222/#223/#224, not current `main` behavior.

## PR #221 Approval-Parity Proof

Fresh proof in `/home/orchestrator/odin-os/.worktrees/high-risk-approval-parity`:

```bash
go test ./internal/app/lifecycle -run TestRunWorkReadOnlyHighRiskCategoriesRequireApprovalThroughOperatorPath -count=1
go test ./internal/runtime/jobs -run 'Test(ExecuteNextQueuedTreatsHighRiskReadOnlyTaskAsApprovalRequired|ClassifyTaskExecutionIntentCoversHighRiskRealWorldMutationCategories|DispatchTaskRunAttemptBlocksGovernanceAndDestructiveIntentsForApproval)' -count=1
make build
```

Real `./bin/odin` proof then ran against a fresh `ODIN_ROOT`:

- selected `odin-core`
- set transition state to `cutover`
- created each high-risk Work Item with `--intent read_only`
- dispatched each Work Item with `odin work dispatch --task <key> --json`
- read back approvals with `odin approvals all --json`
- read back audit events with `odin logs --json`

Categories proven:

| Category | Persisted intent | Result |
| --- | --- | --- |
| Send message to customer | `governance` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |
| Delete data from customer records | `destructive` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |
| Deploy code to production | `governance` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |
| Change calendar event with client | `governance` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |
| Publish public launch post | `governance` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |
| Modify production system config | `governance` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |
| Make purchase of subscription | `governance` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |
| Change permissions for repository | `governance` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |
| Update financial record | `governance` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |
| Update legal records | `governance` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |
| Update medical record | `governance` | blocked with `approval_required` and `execution_intent_source=safety_classifier` |

`odin approvals all --json` showed 11 pending approvals, and `odin logs --json`
showed 11 `approval.requested` events. This closes the approval-parity proof in
the open PR. It still does not count as current `main` behavior until PR #221 is
merged or explicitly accepted as the target artifact.

## PR #218 Capability Proof

Fresh proof in `/home/orchestrator/odin-os/.worktrees/capabilities-operator-cli-current`
after `make build`:

```bash
tmp=$(mktemp -d)
home=$(mktemp -d)
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin capabilities list --kind command --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin capabilities show project.status --json
rm -rf "$tmp" "$home"
```

Observed proof:

- `capabilities list --kind command --json` returned
  `source=capability_gateway` and
  `plugin_model=plugins_are_packages_not_runtime_kind`.
- The command list included `project.status` with `kind=command`,
  `version=1.0.0`, and `scope=global`.
- `capabilities show project.status --json` returned the same source and plugin
  model fields, with a registry-backed descriptor:
  `implementation.kind=markdown`,
  `implementation.path=registry/commands/project.status.md`, and source path
  rooted in the PR #218 worktree.

This closes the plugin-model design in the open PR without introducing a
parallel plugin runtime. It still does not count as current `main` behavior
until PR #218 is merged or explicitly accepted as the target artifact.

## Real Odin Overview Proof

Fresh proof on the doc-only audit branch after `make build`:

```bash
tmp=$(mktemp -d)
home=$(mktemp -d)
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin project select odin-core
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin transition set cutover confirm because audit-overview-current-main
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin trigger create overview-daily initiative=odin-core kind=schedule status=enabled cadence=1h next=2026-05-05T03:00:00Z title=Overview_daily summary=overview_review intent=governance --json
start_output=$(ODIN_ROOT="$tmp" HOME="$home" ./bin/odin work start --project odin-core --title Send_message_to_customer --intent read_only)
task_key=$(printf '%s\n' "$start_output" | tr ' ' '\n' | sed -n 's/^key=//p')
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin work dispatch --task "$task_key" --json
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin overview
ODIN_ROOT="$tmp" HOME="$home" ./bin/odin overview --json
rm -rf "$tmp" "$home"
```

Observed text overview proof:

- `Attention` showed `approvals=1`, `blocked_work=1`, and the pending Approval
  Request for the blocked Work Item.
- `Review Queue` showed `wiring=live total=1 approvals=1`.
- `Active Execution` showed `runs=0` and no active Run Attempts for the blocked
  item.
- `Work Items` showed the blocked Work Item and nested `Run Attempts`.
- `Approvals` repeated the pending approval with resolver support.
- `Observability` showed `activity_log=9`, `blocked_work=1`, `Activity Log`,
  `Run Attempts`, `Blocked Work`, `Incidents`, and `Recoveries`.
- `Intake Inbox` showed `wiring=live source=intake_items status=empty`.
- `Automation Triggers` showed the created trigger with
  `next_due_at=2026-05-05T03:00:00Z`.

Observed JSON overview proof included `work_items`, `review_queue`,
`approvals`, `observability`, `blocked_work`, `recovery_guidance`,
`intake_inbox`, and `automation_triggers` keys.

Fresh failed-work current-main proof:

```bash
rg -n "Failed Work|RecoveryGuidance|failed work|retry_eligible|retry_count|last_error" internal/cli internal/app/lifecycle internal/runtime internal/store tests
go test ./internal/cli/render -run TestRenderOverviewUsesCanonicalLanes -count=1
go test ./internal/app/lifecycle -run TestRunWorkRetryBlocksAtMaxAttemptsWithGuidance -count=1
```

Observed proof:

- Current `internal/cli/overview/service.go` has
  `Observability.RecoveryGuidance` and retry-guidance fields.
- Current `TestRunWorkRetryBlocksAtMaxAttemptsWithGuidance` passes and asserts
  `overview --json` includes `recovery_guidance`,
  `decision=retry_blocked_max_attempts`, and the failed Work Item key.
- Current `internal/cli/render/overview.go` does not render a dedicated
  `Failed Work` section from `view.Observability.RecoveryGuidance`.
- Current `TestRenderOverviewUsesCanonicalLanes` passes with a
  `RecoveryGuidance` fixture but only asserts the review-queue `failed=1`
  summary, not a human-readable failed-work row.

This proves current `main` already carries failed-work recovery data in JSON,
but not the operator-facing text/TUI failed-work lane required by the objective.
That remains the purpose of PR #216.

## PR #216 Failed-Work Proof

Fresh proof in `/home/orchestrator/odin-os/.worktrees/overview-failed-work-lane-current`:

```bash
go test ./internal/cli/render -run TestRenderOverviewUsesCanonicalLanes -count=1
go test ./internal/app/lifecycle -run 'TestRunOverview|TestRunWorkExecuteSurfacesRepoDriverFailure|TestRunWorkExecuteSurfacesFailedDispatchedRun' -count=1
```

Observed proof:

- `internal/cli/render/overview.go` renders a dedicated `Failed Work` section
  from `view.Observability.RecoveryGuidance`.
- `TestRenderOverviewUsesCanonicalLanes` asserts `Attention` and
  `Observability` counts include `failed_work=1`.
- The same renderer test asserts the row
  `failed_work=alpha-task project=alpha companion=primary status=failed
  decision=retry_allowed retry_eligible=true retries=1/3 source=codex
  last_error=driver failed proof recommendation=retry from review queue`.
- Focused lifecycle tests for overview and failed dispatched/repo-driver runs
  pass in the PR #216 worktree.

This closes the failed-work overview rendering gap in the open PR. It still
does not count as current `main` behavior until PR #216 is merged or explicitly
accepted as the target artifact.

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

## Evidence Snapshot

Latest read-only refresh: 2026-05-11T20:19:31Z.

- `origin/main`: `eb51743e9321724b561a4b01aad674ed08939e77`
- audit branch: `815afb1547fac0a28d61843214909e1b51946d07`
- PR #220 state: draft, merge state `CLEAN`, GitGuardian, two `go` checks, and
  `odin-e2e` passing after the approval-parity proof update.

Current objective stack status:

| PR | State | Merge state | Checks |
| --- | --- | --- | --- |
| #211 | draft | `CLEAN` | GitGuardian, two `go`, and `odin-e2e` passing |
| #212 | ready for review | `CLEAN` | GitGuardian, two `go`, and `odin-e2e` passing |
| #213 | draft | `CLEAN` | GitGuardian, two `go`, and `odin-e2e` passing |
| #214 | draft | `CLEAN` | GitGuardian and two `go` passing |
| #216 | draft | `CLEAN` | GitGuardian, two `go`, and `odin-e2e` passing |
| #218 | draft | `CLEAN` | GitGuardian, two `go`, and `odin-e2e` passing |
| #219 | draft | `CLEAN` | GitGuardian, two `go`, and `odin-e2e` passing |
| #220 | draft | `CLEAN` | GitGuardian, two `go`, and `odin-e2e` passing |
| #221 | draft | `CLEAN` | GitGuardian, two `go`, and `odin-e2e` passing |
| #222 | draft | `CLEAN` | GitGuardian, two `go`, and `odin-e2e` passing |
| #223 | draft | `CLEAN` | GitGuardian and two `go` passing |
| #224 | draft | `CLEAN` | GitGuardian and two `go` passing |

## Operator Decision Packet

The next step must choose exactly one path.

Because both paths cross an operational boundary, generic responses such as
`yes`, `proceed`, or `continue` are not enough. The operator decision must name
the selected path and scope.

Approval wording that counts:

- Option A live smoke:
  `Approve Option A: run PR #224 live smoke against <owner/repo> branch <branch>.`
  The approval must also confirm that the repository and branch are disposable
  and that `GITHUB_TOKEN` is scoped only for the accepted pull-request write.
- Option B integration:
  `Approve Option B: integrate the green draft stack using the documented
  runbook.`
  The approval may narrow the scope to named PRs, for example
  `Approve Option B for PRs #212, #213, and #214 only.`

Replies that do not count as approval:

- `yes`
- `proceed`
- `continue`
- approval that does not name Option A or Option B
- approval that names Option A but omits the disposable repository, branch, or
  token-scope confirmation

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

#### Option B Execution Runbook

Do not run these mutation steps without explicit operator approval to integrate
the draft stack.

Read-only preflight before any merge or retarget:

```bash
gh pr list --state open --json number,title,headRefName,baseRefName,isDraft,mergeStateStatus,statusCheckRollup
gh pr view 220 --json mergeStateStatus,statusCheckRollup
```

For every PR selected for integration, verify before merging:

```bash
gh pr view <number> --json isDraft,mergeStateStatus,statusCheckRollup,body
gh pr checks <number> --watch
```

Required PR body headings before merge:

- `## Summary`
- `## Proven`
- `## Unproven`
- `## Commands Run`

Recommended bottom-up integration sequence after approval:

1. Integrate #212.
2. Integrate #213, then retarget or integrate #214 against current `main`.
3. Integrate the independent objective closures #216, #218, and #221.
4. Retarget or refresh #219 against current `main`; verify that
   `odin work proof` and `odin work pr prepare` evidence still passes.
5. Retarget or refresh #222 so its delivery and prompt-to-production
   prerequisites are explicit rather than silently carried.
6. Refresh #223 on top of the accepted #222 state.
7. Refresh #224 on top of #223, then either run the approved live smoke or keep
   it draft until live proof is approved.

Permitted integration mutations after approval:

- changing PR base branches
- pushing conflict-resolution or rebase commits needed to preserve the already
  proven behavior
- merging approved PRs whose checks are green and whose body contract is intact

Still forbidden during integration:

- GitHub merge of delivery output produced by Odin itself
- deployment system calls
- production mutation
- branch deletion
- release creation
- batch approval
- running PR #224 live smoke without the explicit disposable repo, branch, and
  token approval described in Option A

Post-merge proof after each integrated PR:

```bash
git fetch origin main
gh pr view <number> --json state,mergedAt,mergeCommit
gh pr list --state open --json number,headRefName,baseRefName,mergeStateStatus,statusCheckRollup
```

Final integration proof before calling the objective complete:

- real `odin trigger create/list/show/test` proof, or merged evidence that those
  commands are already on `main`
- real high-risk `odin work dispatch --task` approval-blocking proof on `main`
- real `odin capabilities list/show` plugin-model proof on `main`
- real `odin overview` and `overview --json` proof for raw intake, review
  queue, triggers, approvals, recovery, running work, failed work, and blocked
  work on `main`
- real prompt-to-production `odin work proof`, PR handoff, review-role Run
  Attempt, and merge/deploy approval-request proof on `main`
- either approved PR #224 live-smoke evidence, or an explicit accepted decision
  that fixture/local PR-handoff proof is sufficient before live smoke

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
