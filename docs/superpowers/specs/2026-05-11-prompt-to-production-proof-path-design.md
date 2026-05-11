---
title: Prompt-To-Production Proof Path Design
date: 2026-05-11
status: approved-for-implementation-planning
scope: odin-os prompt-to-production v1 proof path
---

# Prompt-To-Production Proof Path Design

## Purpose

Odin's prompt-to-production promise must be proven by durable runtime objects and
operator-visible gates, not by a registry prompt claiming that work is complete.

This slice defines the v1 proof path from a vague GitHub issue or operator goal
to clarification, reviewed ticket/spec, Work Item, Run Attempt evidence, draft
pull request handoff, human review, and explicit approval before merge or
deployment.

The first implementation slice should add a read-only proof command that
correlates existing evidence. It must not create branches, run workers, open
pull requests, merge, deploy, mutate GitHub, or bypass approvals.

## Audit Summary

Audited current `origin/main` in the isolated worktree at
`/home/orchestrator/odin-os/.worktrees/trigger-operator-workflow-current`.

Read and compared:

- `AGENTS.md`
- `docs/contracts/github-tracker-mutations.md`
- `docs/contracts/work-execution-state.md`
- `docs/contracts/verification-model.md`
- `docs/contracts/tui-overview.md`
- `docs/SECURITY.md`
- `docs/ARCHITECTURE.md`
- `docs/brownfield/ARCHITECTURE_GAP_ANALYSIS.md`
- `docs/ROADMAP.md`
- `docs/superpowers/specs/2026-05-11-governed-intake-review-spine-design.md`
- `docs/superpowers/specs/2026-05-10-jobs-runs-work-execution-design.md`
- `docs/superpowers/specs/2026-05-10-approval-gated-execution-policy-parity-design.md`
- `internal/app/lifecycle/run.go`
- `internal/app/lifecycle/review.go`
- `internal/app/lifecycle/review_sources.go`
- `internal/runtime/jobs/service.go`
- `internal/review/pull_request.go`
- `internal/review/handoff.go`
- `internal/review/github_pull_request.go`
- `internal/store/sqlite/migrations/0043_pull_request_handoffs.sql`
- `internal/tracker`
- `internal/vcs`

Also checked current open work that must not be duplicated:

- PR #213 adds `odin work evidence`.
- PR #214 adds `odin work advance`.
- PR #216 adds failed-work overview surfacing.
- PR #218 adds the capabilities operator CLI and locks the plugin model.

## Existing State

Odin already has most control-plane pieces needed for the proof path:

- `odin intake raw create/list/show`
- `odin intake process`
- `odin intake review list/show/accept/reject/clarify/archive`
- unified `odin review`
- Work Items in SQLite `tasks`
- Run Attempts in SQLite `runs`
- approval requests and approval resolution
- trigger-created Work Items with event envelopes and dedupe keys
- runtime events and JSON logs
- `odin logs`, `odin jobs`, `odin runs`, `odin approvals`, `odin overview`
- Git worktree leases, branch naming, and git adapters
- `internal/review.HandoffOrchestrator`
- `pull_request_handoffs` and `pull_request_review_results`
- GitHub PR manager code that can create/update pull requests, with dry-run and
  token redaction behavior

The current contracts also define strict safety boundaries:

- GitHub issue labels, comments, and follow-up issues are projections, not
  runtime authority.
- The GitHub tracker mutation contract explicitly does not cover pull request
  creation, pull request update, review, merge, branch deletion, or deployment.
- Human review handoff is not merge approval or deployment approval.
- Workers must not decide merge, production deployment, or approval state.
- Dry-run mode must not create branches, worktrees, pull requests, labels, or
  comments.

## Partial Or Contradictory State

Prompt-to-production is a recorded product goal, but current implementation is
not yet end-to-end:

- Intake can produce clarification or review-required artifacts, but no single
  operator proof command correlates the original prompt, review decision, Work
  Item, Run Attempt, approval state, PR handoff, and final human gate.
- `internal/review` has a PR handoff seam and GitHub PR manager, but there is no
  approved operator command that wires it into the work lifecycle.
- PR creation/update exists as package code, but the GitHub tracker mutation
  contract excludes PR mutation and says live mutation needs separate approval
  contracts before implementation.
- The brownfield gap analysis still marks PR manager and merge/deploy approval
  enforcement as high-risk gaps.
- Open PRs #213 and #214 are adding delivery evidence and delivery gate
  advancement; this design should compose those surfaces after they land instead
  of duplicating them.

## Reused Components

The proof path should reuse:

- `odin intake raw` for source evidence
- `odin intake process` for classification, dedupe, route, and draft artifact
  creation
- `odin intake review` and unified `odin review` for clarification, acceptance,
  rejection, archive, and approval visibility
- `odin work status|start|dispatch|execute|retry`
- future `odin work evidence` from PR #213
- future `odin work advance` from PR #214
- `odin jobs`, `odin runs`, `odin approvals`, `odin logs`, and `odin overview`
- `internal/runtime/jobs.Service`
- `internal/runtime/approvals.Service`
- `internal/runtime/projections`
- SQLite `intake_items`, `tasks`, `runs`, `approvals`, `events`,
  `pull_request_handoffs`, and `pull_request_review_results`
- `internal/review.BuildPullRequestBody`
- `internal/review.HandoffOrchestrator`
- `internal/review.PullRequestManager`
- `internal/vcs` branch, git, worktree, and lease packages
- `internal/tracker` for GitHub issue intake and issue-projection boundaries

## New Components

Add one narrow read-only proof surface:

- `odin work proof --task <id|key> [--json]`

This command should assemble a `prompt_to_production_proof` read model from
existing runtime state. It should not mutate state in v1.

The read model should include:

- source intake item, source URL, source type, requested-by, and dedupe key when
  linked
- clarification status and outstanding questions when intake is unclear
- reviewed draft/ticket/spec status when intake is review-required
- Work Item status, key, execution intent, blocked reason, and acceptance
  criteria
- Run Attempt status, executor, attempt number, terminal summary, and artifacts
- delivery evidence status when `odin work evidence` exists
- delivery gate state when `odin work advance` exists
- approval request status and approval reason when work is high-risk or blocked
- pull request handoff status when present
- review result status for QA, reviewer, and security roles when present
- merge gate status
- deployment gate status
- event correlation summary across intake, review, task, run, approval, and PR
  handoff evidence

No new durable authority table is required for v1. If the query is expensive or
awkward after command proof, a projection may be added later, but the projection
must derive from existing authorities and never become a second workflow state
machine.

## Why New Components Are Necessary

Current commands prove individual pieces, but operators need one truth-preserving
answer to the question: "Can this vague issue or goal be safely treated as ready
for review, PR, merge, or deploy?"

A read-only proof command is necessary because:

- it gives one operator surface for end-to-end evidence without introducing a
  new automation path;
- it can expose missing links and stale evidence before any GitHub or deployment
  mutation exists;
- it composes open delivery-evidence work without duplicating it;
- it preserves the current rule that no registry prompt, YAML file, or design
  document counts as automation until a real `odin` command invokes it,
  persists the result, enforces policy, and emits audit evidence.

## Locked Domain Decisions

- Canonical phrase: `prompt-to-production proof path`.
- The proof path is a correlation of existing durable objects, not a new
  workflow runtime.
- `drafted` remains pre-work state owned by intake, review, proposal, or ticket
  generation.
- `approved` remains a source-owned decision or Approval Request outcome, not a
  Work Item status.
- `ready_for_review` means evidence is assembled for human or reviewer-agent
  inspection; it does not approve merge or deployment.
- `ready_for_merge` is true only when the PR handoff exists, required review
  evidence exists, checks are represented, and a human merge approval is still
  explicitly required or already recorded.
- `ready_for_deploy` is true only when deployment is in scope, deployment
  evidence exists, and a human deployment approval is still explicitly required
  or already recorded.
- GitHub issues and pull requests remain external collaboration artifacts.
  SQLite remains runtime authority.
- A pull request handoff is not approval to merge or deploy.
- Merge and production deploy remain forbidden for unattended automation.
- Live PR creation/update needs its own approval-gated operator contract before
  it can be wired to `internal/review.GitHubPullRequestManager`.
- The v1 proof command must be read-only and safe against a fresh temporary
  `ODIN_ROOT`.

## Selected Design

Implement a read-only command that reports the end-to-end proof state for one
Work Item.

### State Buckets

The command should return stable bucket names:

| Bucket | Meaning |
| --- | --- |
| `needs_clarification` | source intake exists but questions remain open |
| `drafted` | source artifact exists, but review has not accepted it |
| `review_required` | human or source-owned review decision is required |
| `approval_required` | Approval Request is pending before execution or external mutation |
| `queued` | Work Item exists and is waiting for dispatch |
| `running` | Work Item has an active Run Attempt |
| `evidence_required` | implementation ran but required delivery evidence is missing |
| `review_ready` | evidence and PR handoff are ready for human/reviewer inspection |
| `merge_approval_required` | PR may be merge-ready, but human merge approval is required |
| `deploy_approval_required` | deployment may be ready, but human deployment approval is required |
| `completed` | Odin-owned work is terminal; external merge/deploy may still be out of scope |
| `blocked` | proof cannot advance; blocker is named in the output |
| `failed` | Work Item or Run Attempt failed and failure evidence is visible |

### JSON Shape

The `--json` response should include:

```json
{
  "schema": "prompt_to_production_proof.v1",
  "task": {
    "id": 1,
    "key": "odin-core-example",
    "status": "blocked",
    "execution_intent": "mutation",
    "blocked_reason": "approval_required"
  },
  "source": {
    "type": "intake_item",
    "id": 1,
    "dedupe_key": "github:issue:acme/odin-os:77",
    "url": "https://github.example/acme/odin-os/issues/77"
  },
  "proof_state": "approval_required",
  "clarification": {
    "status": "answered",
    "questions": []
  },
  "review": {
    "status": "accepted",
    "queue_id": "intake-review:intake-1"
  },
  "execution": {
    "runs": [],
    "active_run_id": null
  },
  "delivery": {
    "evidence_status": "missing",
    "gate_status": "not_started"
  },
  "pull_request": {
    "status": "missing",
    "handoff_id": null,
    "url": ""
  },
  "approvals": {
    "pending": [],
    "resolved": []
  },
  "merge_gate": {
    "status": "not_ready",
    "human_approval_required": true,
    "approved": false
  },
  "deployment_gate": {
    "status": "not_in_scope",
    "human_approval_required": true,
    "approved": false
  },
  "events": {
    "count": 0,
    "latest": []
  },
  "next_steps": [
    "record delivery evidence before PR handoff"
  ],
  "mutated": false
}
```

### Command Behavior

- `odin work proof --task <id|key>` prints a concise human-readable proof
  summary.
- `odin work proof --task <id|key> --json` prints the stable JSON envelope.
- Missing optional evidence should be represented as `missing` or
  `not_started`, not as an error.
- Unknown Work Item should return a stable not-found error and make no
  mutations.
- A Work Item with no source intake should still produce a proof based on task,
  run, approval, and PR handoff state.
- The command must include `mutated=false`.

### Relationship To PR Handoff

The read-only proof command may read PR handoff rows.

It must not call `PullRequestManager.Upsert`, `AddComment`, `gh`, GitHub API, or
any deployment command.

Live PR creation/update should be a later separate design that:

1. builds an exact dry-run PR mutation bundle from existing Odin evidence;
2. creates an Approval Request for the exact bundle;
3. revalidates task, run, branch, handoff, target repo, token scope, and approval
   immediately before write;
4. writes only the approved bundle;
5. records runtime events and PR handoff evidence;
6. leaves merge and deploy approvals separate.

## Rejected Alternatives

### Build a new prompt-to-production workflow table

Rejected. Work Items, Run Attempts, approvals, review entries, intake items,
events, and PR handoff rows are already the durable authorities.

### Wire live PR creation in the first slice

Rejected. The repo has PR manager package code, but no approved operator contract
for live PR mutation. The first slice should prove correlation and missing
evidence before creating external writes.

### Treat PR handoff as merge approval

Rejected. The security contract explicitly says a handoff is not approval to
merge or deploy.

### Reuse GitHub labels as runtime state

Rejected. GitHub labels are projections only and must never pause, resume,
dispatch, approve, or complete Odin work.

### Duplicate open delivery evidence work

Rejected. PR #213 and PR #214 already cover delivery evidence and gate
advancement. This design composes those surfaces after they land.

## Test And Verification Plan

Focused implementation tests:

```bash
go test ./internal/app/lifecycle -run 'TestRunWorkProof|TestRunIntakeProcessCreatesReviewStatesWithoutExecution|TestRunUnifiedReviewQueue' -count=1
go test ./internal/review ./internal/store/sqlite -run 'PullRequest|Handoff' -count=1
```

Real command proof:

```bash
make build
tmp="$(mktemp -d)"
printf '{"original_content":"build a vague GitHub issue into a safe proof path"}' | ODIN_ROOT="$tmp" ./bin/odin intake raw create --source github --title 'Vague issue proof' --type github_issue --dedup-key github-issue-proof --requested-by codex --payload-file - --json
ODIN_ROOT="$tmp" ./bin/odin intake process --id intake-1 --json
ODIN_ROOT="$tmp" ./bin/odin intake review list --json
ODIN_ROOT="$tmp" ./bin/odin work status --json
ODIN_ROOT="$tmp" ./bin/odin work proof --task <created-or-accepted-task> --json
ODIN_ROOT="$tmp" ./bin/odin jobs --json
ODIN_ROOT="$tmp" ./bin/odin runs --json
ODIN_ROOT="$tmp" ./bin/odin approvals all --json
ODIN_ROOT="$tmp" ./bin/odin logs --json
rm -rf "$tmp"
```

Failure-path proof:

```bash
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin work proof --task missing-task --json
rm -rf "$tmp"
```

The implementation is done only when the command proves that it made no
mutations and shows honest missing evidence for incomplete workflows.

## Implementation Hardening Status

PR #219 implements the v1 read-only proof command:

- `odin work proof --task <id|key> [--json]`
- `prompt_to_production_proof.v1` JSON envelope
- source intake correlation through reviewed intake Work Items
- Run Attempt, approval, PR handoff, merge gate, deployment gate, and task event
  readback
- PR handoff evidence for provider, repo, number, branch, tests, risks,
  blockers, selected review roles, and reviewer/QA/security result rows when
  they exist
- honest `missing` and `not_started` states for incomplete delivery and PR
  evidence
- fail-closed unknown Work Item handling
- `mutated=false` with command-level proof that no log event is appended

The implementation does not add live PR creation/update, merge approval
resolution, deployment approval resolution, worker dispatch, branch creation, or
GitHub mutation.

## Documentation Changes

Add this design spec.

During implementation, update:

- `docs/contracts/work-execution-state.md` with the proof command
  responsibility.
- `docs/contracts/verification-model.md` only if the implementation introduces
  a new proof-report convention.
- `docs/contracts/github-tracker-mutations.md` only in a later PR mutation
  contract slice.

No ADR is required for this slice because the selected design deepens existing
authority boundaries rather than making a hard-to-reverse architectural change.

## Security Review

The v1 command must be read-only.

It must not:

- create branches or worktrees;
- start workers or dispatch Work Items;
- write PRs, comments, labels, reviews, checks, releases, or deployments;
- resolve approvals;
- treat GitHub issue or PR state as runtime authority;
- expose GitHub tokens, env values, or credential material;
- mark merge or deploy as approved without an Odin-owned approval record.

The command should fail closed for malformed task selectors and should redact any
external error text if future sources are added.

## Open Blockers

- PR #213 and PR #214 are open and may add the preferred delivery evidence and
  gate fields that `odin work proof` should read.
- There is no approved live PR mutation contract for wiring
  `GitHubPullRequestManager` to an operator command.
- Merge and deployment approval resolvers are not implemented end-to-end.
- Reviewer, QA, and security role execution is not fully implemented as runtime
  Run Attempts.

## Planning Handoff

Implement one PR-sized read-only proof slice:

1. Add tests for `odin work proof --task <id|key> --json` over a temporary
   `ODIN_ROOT`.
2. Add a small proof assembler in `internal/app/lifecycle` or the nearest
   existing work command handler.
3. Reuse existing store queries first; add narrow store list/get helpers only
   when a required authority has no read path.
4. Add `work proof` to the existing `odin work` command group.
5. Include honest `missing` and `not_started` states for PR handoff, delivery
   evidence, and review roles that do not exist yet.
6. Update the work execution contract with the new command responsibility.
7. Prove through focused tests and real `./bin/odin` commands.

## Implementation Goal Prompt

```text
/goal Implement the read-only prompt-to-production proof command in /home/orchestrator/odin-os.

Use docs/superpowers/specs/2026-05-11-prompt-to-production-proof-path-design.md as the approved design. Keep the work PR-sized and make atomic commits. Reuse odin intake, odin review, odin work, odin jobs, odin runs, odin approvals, odin logs, internal/app/lifecycle, internal/runtime/jobs, internal/runtime/projections, internal/review, SQLite intake_items/tasks/runs/approvals/events/pull_request_handoffs, and existing store helpers. Do not introduce a new workflow runtime, queue, policy engine, PR mutation command, merge command, deploy command, or GitHub label authority.

Required behavior:
- add `odin work proof --task <id|key> [--json]`
- return a `prompt_to_production_proof.v1` envelope with source, proof_state, clarification, review, execution, delivery, pull_request, approvals, merge_gate, deployment_gate, events, next_steps, and `mutated=false`
- represent missing optional evidence honestly as `missing` or `not_started`
- fail closed for unknown task selectors
- do not make external network calls or state mutations
- update docs/contracts/work-execution-state.md with the command proof responsibility

Required verification:
- go test ./internal/app/lifecycle -run 'TestRunWorkProof|TestRunIntakeProcessCreatesReviewStatesWithoutExecution|TestRunUnifiedReviewQueue' -count=1
- go test ./internal/review ./internal/store/sqlite -run 'PullRequest|Handoff' -count=1
- make build
- with a temporary ODIN_ROOT, prove intake raw create, intake process, review list, work status, work proof --json, jobs --json, runs --json, approvals all --json, and logs --json
- prove `odin work proof --task missing-task --json` fails closed without mutation

Delivery:
- preserve unrelated dirty worktree changes
- open a PR with Summary, Proven, Unproven, and Commands Run
- monitor checks
- fix failures in follow-up atomic commits
- do not merge or deploy without explicit approval
```
