# Autonomous Intake Promotion Design

Date: 2026-05-17
Status: active implementation slice

## Objective

Make Odin stop routing every actionable raw Intake Item through the Review queue before any Work Item exists, while preserving approval gates for unsafe work.

## Existing State

`odin intake process` already classifies raw intake, derives intent, writes audit events, and creates review artifacts. `odin intake review accept` already promotes acceptable low-risk intake into Work Items. `odin work dispatch` and `odin serve` already execute queued Work Items under admission policy.

The gap is that processing always leaves clear low-risk intake in `review_required`, so harmless read-only work still needs an operator review action before Odin can work autonomously.

## Design

Reuse the existing promotion path during processing for only direct-work-safe intake:

- status is `review_required`
- scope is a registered Managed Project
- route is a read-only draft task, idea, research, incident review, routine, or follow-up
- item is not goal-like, duplicate, ambiguous, archive-candidate, or skill-bound
- `intakePromotionPolicy` does not require approval

When those conditions hold, processing creates the Work Item and records the same `intake.review_accepted` audit event family with `decision=auto_accepted`.

Everything else stays governed:

- mutation remains in Review
- governance and destructive work remain approval-gated
- risk-marked text such as production, credentials, secrets, payment, deploy, and delete does not auto-promote
- processing never dispatches, executes, creates branches, opens PRs, or mutates external systems

## Verification

Required proof:

- targeted lifecycle tests for auto-promotion and preserved gates
- focused command tests for intake/work/job state
- `make build`
- temporary `ODIN_ROOT` proof with real `./bin/odin intake raw create`, `intake process`, `intake review list`, `jobs`, `work status`, and risky intake readback
