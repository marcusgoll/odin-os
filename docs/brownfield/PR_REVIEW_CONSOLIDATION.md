---
title: Odin OS PR And Review Consolidation
status: draft
date: 2026-04-30
---

# Odin OS PR And Review Consolidation

## Current State

Odin-OS has PR and review policy assets, but it does not yet have a live GitHub
PR mutation adapter.

Existing assets to preserve:

- `.github/pull_request_template.md`: required human PR handoff headings.
- `scripts/ci/verify-pr-template.sh`: CI-enforced PR body validation.
- `.github/workflows/ci.yml`: runs `make ci` and verifies PR body shape.
- `registry/skills/pr-review.md`: PR readiness review procedure.
- `registry/skills/qa-review.md`: QA evidence procedure.
- `registry/skills/security-review.md`: security review procedure.
- `prompts/workers/{qa,reviewer,security}.md`: draft worker prompts.
- `config/projects.yaml`: project merge policy with direct default-branch
  mutation disabled.
- `internal/review`: canonical review and PR handoff package.

## Canonical Package

`internal/review` is the canonical package for PR handoff and review-selection
logic.

Canonical interface:

- `review.PullRequestManager`

Canonical helpers:

- `review.BuildPullRequestBody`
- `review.BuildReviewComment`
- `review.SelectReviewAgents`

`PullRequestManager` intentionally has no merge, approval, or deployment method.
Human approval remains required before merge or production deployment.

## Security Review Triggers

Security review is required for changes touching:

- runners or executor launch paths
- shims or shell scripts
- process execution
- filesystem operations or worktree management
- GitHub tokens or GitHub API writes
- dashboard/admin controls
- secrets or runtime config
- deployment files or GitHub Actions automation

Review agents selected by `internal/review` are read-only. A reviewer, QA, or
security run may report findings and blockers, but must not approve, merge, or
deploy.

## Duplicate Or Placeholder Paths

| Path | Status | Decision | Notes |
| --- | --- | --- | --- |
| `internal/review` | Active canonical package | keep | Deepen this package before adding live PR adapters. |
| `internal/workers/qa` | Empty worker placeholder | refactor | Implement only after executor/review orchestration is ready. |
| `internal/workers/reviewer` | Empty worker placeholder | refactor | Reuse `internal/review` selection and handoff helpers. |
| `prompts/workers/qa.md` | Draft prompt | keep | Prompt text remains available for future QA worker. |
| `prompts/workers/reviewer.md` | Draft prompt | keep | Prompt text remains available for future reviewer worker. |
| `prompts/workers/security.md` | Draft prompt | keep | Prompt text remains available for future security worker. |
| `internal/adapters/github` | Empty placeholder | remove later | Do not create a PR adapter here unless the GitHub adapter root decision changes. |
| `internal/tracker/github` | Existing issue tracker adapter | keep | PR mutation should not be bolted into issue intake without a follow-up design. |

## Follow-Up Work

1. Add a live GitHub PR adapter behind `review.PullRequestManager`.
2. Wire review selection into the orchestration loop after PR handoff exists.
3. Persist PR handoff and review results in SQLite.
4. Add read-only reviewer, QA, and security run attempts behind the executor
   contract.
5. Add a live GitHub proof ticket before enabling PR create/update outside
   fixture-backed tests.
