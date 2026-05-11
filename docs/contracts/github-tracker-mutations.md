---
title: GitHub Tracker Mutation Approval Contract
status: active
date: 2026-05-09
---

# GitHub Tracker Mutation Approval Contract

This contract defines when Odin may mutate GitHub issue state through the
canonical `internal/tracker` seam.

It does not implement live mutation wiring. It exists so future implementation
work can add label, comment, and follow-up issue writes without turning GitHub
into runtime authority or bypassing Odin approvals.

## Scope

This contract covers outbound GitHub issue mutations exposed by
`internal/tracker.Tracker`:

- adding Odin projection labels
- adding issue comments
- creating follow-up GitHub issues
- closing an issue only when a separate close approval explicitly authorizes it

It does not cover:

- pull request creation, update, review, merge, or branch deletion, except for
  the separate `odin work pr prepare` handoff path governed by
  `docs/contracts/pull-request-handoff-mutations.md`
- production deployment
- repository settings, workflow, secret, release, or environment mutation
- deleting GitHub issues, comments, or labels
- editing human-authored GitHub issue bodies or comments

Those actions require separate contracts or ADRs before implementation.

## Current Implementation Status

`internal/tracker` is the canonical GitHub issue and pull request tracker seam.
`internal/tracker/github.Client` already has methods that can call the GitHub
Issues API when configured with a token and `DryRun=false`.

Current operator work intake remains read-only:

- `odin work intake` may fetch and persist eligible external issue facts.
- `odin work reconcile` may create or reuse Odin Work Items from persisted
  external issue facts.
- Neither command may dispatch workers, create branches, create pull requests,
  add comments, create follow-up issues, close issues, or mutate labels.

Until a future implementation adds this approval contract to an operator command
or runtime service, tracker mutation methods must not be invoked by unattended
automation.

## Authority

SQLite remains the runtime authority.

- Work Item, Run Attempt, Approval Request, Follow-Up Obligation, external issue
  source facts, and runtime events are Odin-owned state.
- GitHub labels, comments, issue state, and follow-up issues are external
  projections or communication artifacts.
- GitHub labels must never become scheduler truth.
- GitHub issue state must never replace Odin Work Item state.
- GitHub comments must never replace Odin run evidence, approval records, or
  runtime events.

GitHub is optional and additive for managed projects. A project without GitHub
metadata must still be governable through local Git and SQLite state.

## Allowed Mutations

Every allowed mutation requires an approved Odin Approval Request before network
write. A dry-run proposal may be generated without approval only if it does not
write to GitHub.

| Mutation | Allowed after approval | Required Odin basis | Notes |
| --- | --- | --- | --- |
| Add `odin:running` | Yes | Work Item has a selected Run Attempt that started under Odin governance. | Projection only; it must not make GitHub the run authority. |
| Add `odin:blocked` | Yes | Work Item is blocked in SQLite with an Odin-owned reason. | If a comment is included, the approved bundle must contain the exact comment body. |
| Add `odin:failed` | Yes | Run Attempt or Work Item failed in SQLite with durable evidence. | Comment text must summarize existing Odin evidence, not invent new findings. |
| Add `odin:human-review` | Yes | Odin has produced review-ready handoff evidence. | Does not approve, merge, or deploy anything. |
| Add or remove `odin:paused` | Yes | Work Item pause/resume already happened in SQLite. | Projection only; inbound label changes never pause or resume work. |
| Add `odin:done` | Yes | Work Item completed in SQLite and issue-close policy is satisfied. | If issue close is also requested, the close must be explicitly approved. |
| Close GitHub issue | Only with explicit close approval | Either linked PR merge automation already represents closure, or the operator approves direct closure with reason. | Direct close must not stand in for PR merge, deployment, or review. |
| Add issue comment | Yes | Comment body is generated from existing run, approval, review, failure, or handoff evidence. | The exact body must be shown in dry-run and approval detail. |
| Create follow-up GitHub issue | Yes | Odin has a reviewed follow-up proposal or approved failure-analysis recommendation. | This is an external issue artifact, not an Odin Follow-Up Obligation. |

Forbidden unless a later contract or ADR says otherwise:

- autonomous pull request merge
- autonomous production deploy
- deleting or editing GitHub comments
- deleting labels from issues except approved removal of Odin-owned projection
  labels
- creating follow-up GitHub issues from failure analysis without human approval
- using GitHub label state to unblock, dispatch, pause, resume, approve, or
  complete an Odin Work Item

## Approval Protocol

Future mutation wiring must use this sequence:

1. Build a proposed mutation bundle from existing Odin evidence.
2. Produce a dry-run summary that includes provider, repo, issue number, method,
   labels, exact comment body, follow-up title/body/labels, close intent, and
   idempotency key where applicable.
3. Persist an Approval Request in SQLite before any GitHub write.
4. Block or hold the owning Work Item with `blocked_reason = approval_required`
   when the mutation is required for the workflow to continue.
5. Surface the approval through the existing operator approval surfaces.
6. Require an approving operator, decision reason, and still-current target
   evidence before execution.
7. Revalidate the target issue, repo, Work Item, Run Attempt, and approval
   status immediately before network write.
8. Execute only the approved bundle. A bundle may contain multiple low-level
   GitHub API calls only when the approval listed each call and payload.
9. Persist success or failure evidence in SQLite events and run artifacts.
10. Fail closed on stale approval, target mismatch, missing token, unexpected
    repo, GitHub API error, rate limit, or ambiguous response.

One approval authorizes one explicit bundle. It must not become reusable ambient
GitHub write permission.

Denied approvals must not call GitHub. Expired, superseded, already-resolved, or
unsupported approvals must not call GitHub.

## Dry-Run Contract

Dry-run mode must be available before any live mutation path is enabled.

Dry-run must:

- show the exact mutation bundle Odin would request
- report `dry_run=true`
- avoid all GitHub write requests
- avoid SQLite lifecycle mutation except for explicitly documented preview
  records, if a future implementation adds them
- be safe to run with an unreachable GitHub API base URL for write methods

Dry-run must not:

- create comments
- add or remove labels
- close issues
- create follow-up GitHub issues
- dispatch workers
- create pull requests
- resolve approvals

## Token Boundary

GitHub tokens stay at the tracker adapter boundary.

- Tokens must come from explicit operator environment or service environment.
- Tokens must not be written to config files, prompts, issue bodies, PR bodies,
  comments, logs, screenshots, run artifacts, approval summaries, or worker
  context.
- Approval detail may name the token environment variable, but must never show
  the token value.
- Mutation implementations should require the narrowest repository issue scope
  that can perform the approved bundle.

If the token is missing or broader than the operator accepted for the target
proof, stop before mutation.

## Follow-Up Issue Boundary

A GitHub follow-up issue is an external collaboration artifact.

An Odin Follow-Up Obligation is a durable internal control-plane object.

Creating one must not silently create the other. If a future workflow wants both
objects, the approval bundle must say so explicitly and the implementation must
persist cross-links in SQLite evidence.

Failure analysis may recommend a follow-up issue, but recommendation alone does
not authorize issue creation. A human approval must explicitly approve the
follow-up title, body, labels, target repo, and source evidence.

## Required Tests Before Implementation

Before live mutation wiring is considered complete, tests must prove:

- dry-run label/comment/follow-up paths do not make GitHub write requests
- denied approvals do not make GitHub write requests
- unsupported, expired, stale, or already-resolved approvals do not make GitHub
  write requests
- mutation execution requires an approved Approval Request with a decision maker
  and reason
- the executed labels, comment body, follow-up title/body/labels, and close
  intent match the approved bundle exactly
- target repo and issue number are revalidated immediately before write
- GitHub labels remain projection-only and cannot pause, resume, dispatch,
  approve, or complete a Work Item
- failure-analysis recommendations cannot create follow-up GitHub issues without
  approval
- token-like strings in GitHub errors are redacted from operator output and run
  artifacts
- success and failure paths append Odin runtime evidence

At least one command-level or service-level E2E must prove the real Odin path
against a controlled runtime root. Live GitHub proof remains opt-in and must use
a disposable repository or disposable issue.

## Manual Recovery

GitHub mutations are not fully reversible by Odin because GitHub comments and
issue history are external audit artifacts.

Recovery rules:

- Wrong Odin projection label: remove or correct the label through GitHub UI or
  `gh`, then record the correction in Odin evidence before retrying automation.
- Wrong comment: add a correction comment; do not rely on edit/delete automation.
- Wrong follow-up GitHub issue: close it manually as duplicate or invalid, link
  the mistaken source, and record the cleanup in Odin evidence.
- Wrong issue close: reopen manually, comment with the correction reason, and
  record Odin evidence before retry.
- Wrong repository or token scope: stop mutation wiring, rotate or narrow the
  token if needed, and require fresh approval before any retry.

Retries after manual recovery require a new dry-run proposal and a new approval.

## Operating Rules

- Do not call tracker mutation methods from scheduler, intake, reconciliation,
  workers, or failure analysis without this approval protocol.
- Do not treat a configured `GITHUB_TOKEN` as permission to mutate GitHub.
- Do not pass GitHub tokens to workers.
- Do not use GitHub labels as runtime state.
- Do not introduce autonomous merge or deployment as part of tracker mutation
  work.
- Do not add a second GitHub tracker package outside `internal/tracker`.
