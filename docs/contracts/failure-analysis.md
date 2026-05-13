---
title: Failure Analysis Contract
status: active
date: 2026-04-30
---

# Failure Analysis Contract

## Purpose

Odin records failed work and classifies the likely failure class so an operator
or future worker can choose a safe next step.

Failure analysis is advisory. It must not apply prompt, skill, workflow,
architecture, shim, or implementation changes automatically.

## Runtime Authority

SQLite remains the runtime authority. Failure analysis uses existing task, run,
and event persistence:

- failed runs still finish through `run.finished`
- failed tasks remain visible through existing task status projections
- run artifacts may include a `failure_analysis` object
- retry behavior continues to use existing task retry counters and max attempts

No separate failure database or retry loop should be added.

## Categories

The canonical categories are:

- `unclear_ticket`
- `missing_acceptance_criteria`
- `existing_code_behavior_unknown`
- `characterization_test_missing`
- `test_failure`
- `migration_conflict`
- `dependency_issue`
- `permission_issue`
- `codex_timeout`
- `bad_prompt`
- `bad_skill_selection`
- `conflicting_agent_instructions`
- `unsafe_shim_behavior`
- `security_blocker`
- `merge_conflict`
- `workspace_failure`
- `github_api_failure`
- `dashboard_admin_failure`
- `deployment_failure`
- `unknown`

Readiness failures such as unclear tickets and missing acceptance criteria take
precedence over implementation or test failures.

## Recommendations

Each analysis should include:

- a category
- a short summary
- an actionable suggested fix
- the next target to update: `prompt`, `skill`, `test`, `shim`, `workflow`,
  `architecture`, `implementation`, or `operator`
- whether a follow-up issue is recommended
- whether retry is recommended within the existing attempt budget

Retry recommendations must stop at the configured maximum attempt count.

## Follow-Up Materialization

Failure-analysis follow-ups are approval gated through the existing review
surface:

1. `odin review show failed-work:<task-id> --json` previews the proposed
   follow-up and marks external GitHub issue creation as `not_created`.
2. `odin review act failed-work:<task-id> follow-up --dry-run --json` shows the
   same proposal without writing any follow-up obligation or external issue.
3. `odin review act failed-work:<task-id> follow-up --json` is the explicit
   human approval to create one internal Follow-Up Obligation through the
   existing follow-up persistence layer.

This path does not create GitHub issues. External tracker writes remain
proposal-only until the GitHub tracker mutation contract is implemented and an
approved tracker-mutation bundle exists for the exact write.

## Safety Rules

- Do not hide failed runs.
- Do not retry forever.
- Do not auto-apply workflow changes.
- Do not convert a ticket-readiness failure into an implementation retry.
- Security blockers and unsafe shims require explicit follow-up and human
  review.
- Do not create external GitHub issues from failure analysis without the
  approved tracker-mutation contract.
