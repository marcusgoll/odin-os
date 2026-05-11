---
title: Pull Request Handoff Mutation Contract
status: active
date: 2026-05-11
---

# Pull Request Handoff Mutation Contract

This contract defines the only approved live pull request creation/update path
for Odin work handoff.

## Scope

Covered command:

- `odin work pr prepare --task <id|key> ... --live --approval <id>`

Covered mutation:

- create or update one GitHub pull request for an existing Odin Work Item

Not covered:

- merge
- deploy
- branch deletion
- repository settings
- workflow, secret, release, or environment mutation
- pull request review approval
- GitHub issue labels, issue comments, follow-up issues, or issue closure

## Authority

SQLite remains runtime authority. A GitHub pull request is an external handoff
artifact.

- The Work Item owns task state.
- The Approval Request owns the human decision.
- `pull_request_handoffs` owns the durable handoff record.
- `pull_request_review_results` owns selected review role evidence.
- Runtime events own the audit trail.
- The GitHub pull request must not approve merge or deployment.

## Approval Protocol

Live PR mutation must follow this sequence:

1. Build handoff evidence from the selected Work Item, summary, tests, risks,
   command evidence, blockers, branch, title, and linked intake issue URL.
2. If `--live` is requested without `--approval`, create or reuse a pending
   Approval Request, block the Work Item with `blocked_reason=approval_required`,
   return `approval_required=true`, and make no GitHub request.
3. The operator reviews and resolves that Approval Request through
   `odin approvals resolve`.
4. A later `--live --approval <id>` invocation must verify the approval exists,
   belongs to the same Work Item, and has `status=approved`.
5. The command must fail closed before GitHub I/O if the approval is missing,
   pending, denied, for another task, or already invalid for the target.
6. The command must require `GITHUB_TOKEN` from the operator environment before
   live GitHub mutation.
7. The command may call only the canonical
   `internal/review.PullRequestManager` / `HandoffOrchestrator` seam.
8. Success must persist `pull_request_handoffs`,
   `pull_request_review_results`, and a `pull_request.handoff_prepared` event
   with `external_mutated=true`.

One approval authorizes one explicit Work Item handoff. It must not authorize
merge, deploy, branch deletion, or future GitHub writes.

## Dry-Run And Local Proof

Dry-run/local handoff remains the default. It may persist handoff and review
selection evidence, but it must not call GitHub and must record
`external_mutated=false`.

## Token Boundary

GitHub tokens must come from `GITHUB_TOKEN` or equivalent explicit operator
environment. Token values must not be written to PR bodies, approval summaries,
runtime events, logs, command output, or stored artifacts. GitHub errors must be
redacted before surfacing to operators.

## Required Tests

The live handoff resolver is covered only when tests prove:

- unapproved `--live` creates approval evidence and makes no GitHub request
- approved `--live --approval <id>` calls the GitHub PR manager
- pending, denied, wrong-task, or invalid approvals fail before GitHub I/O
- token-like values are not emitted in command output or logs
- successful live mutation appends `pull_request.handoff_prepared` with
  `external_mutated=true`
- merge and deployment remain separate human approvals
