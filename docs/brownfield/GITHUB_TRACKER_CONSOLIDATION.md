---
title: Odin OS GitHub Tracker Consolidation
status: draft
date: 2026-04-30
---

# Odin OS GitHub Tracker Consolidation

## Current Decision

Use `internal/tracker` as the canonical GitHub Issues/PR tracker seam for the
current migration slice.

The older `internal/adapters/github` directory remains a placeholder and should
not receive new tracker behavior unless a later ADR explicitly moves the seam.

## What Changed

- `internal/tracker.Tracker` now names the target issue lifecycle operations:
  `FetchEligibleIssues`, `FetchIssueByID`, status marking, comments, and
  follow-up issue creation.
- `internal/tracker/github.Client` wraps GitHub REST calls behind that
  interface.
- `ListEligibleIssues` remains as a compatibility method while callers migrate.
- A zero-config `NewClient()` still returns `tracker.ErrNotImplemented`, which
  preserves the previous placeholder behavior.
- Configured clients can fetch open eligible issues and filter labels locally.
- Dry-run clients do not write labels, comments, issue state, or follow-up
  issues to GitHub.
- GitHub API errors are redacted before they leave the adapter.
- `odin work intake --project <key> [--dry-run]` wires configured
  GitHub-backed managed projects into read-only issue intake.
- Read-only intake persists eligible external issues to SQLite in
  `external_issues` with idempotent upsert by provider, repo, and issue number.
- Intake does not dispatch scheduler work, invoke workers, create PRs, add
  comments, or mutate GitHub labels.

## Preserved Labels

The adapter defaults match the existing agency label model:

- Eligible: `odin:ready`
- Blocked: `odin:blocked`
- In progress projection: `odin:running`
- Human review projection: `odin:human-review`
- Failure projection: `odin:failed`
- Done projection: `odin:done`
- Paused projection reserved for later scheduler/runtime support:
  `odin:paused`

The tracker contract also records the current expected agent routing labels:

- `agent:architect`
- `agent:go-orchestrator`
- `agent:backend`
- `agent:frontend`
- `agent:ios`
- `agent:qa`
- `agent:security`
- `agent:reviewer`
- `agent:devops`
- `agent:docs`

GitHub labels remain intake/projection state. SQLite and Odin runtime events
remain the durable authority.

## Security Boundary

- Tokens are read from explicit config or `TokenEnv`.
- Tokens are used only in GitHub API authorization headers.
- Worker prompts do not receive GitHub tokens from this adapter.
- Dry-run mode returns projected results and exits before mutation requests.
- Token-like strings in GitHub errors are replaced with `[REDACTED]`.

## Remaining Cleanup Tickets

1. Add live GitHub proof for read-only intake with an env-gated disposable
   issue/repository runbook.
2. Add live mutation wiring only after approval gates and PR manager contracts
   are specified.
3. Add integration fixtures for `FetchIssueByID`, comments, label mutation, and
   follow-up issue creation.
4. Decide whether `internal/adapters/github` should be removed or retained only
   for non-tracker GitHub APIs.
5. Add rate-limit handling and retry classification for transient GitHub errors.
6. Add PR-specific methods only after the draft PR manager contract is locked.
7. Add explicit paused-state behavior only after scheduler pause/resume semantics
   are specified.
