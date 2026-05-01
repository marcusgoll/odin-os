---
title: Stage 7 Supervised Agency Control Plane Design
status: approved
date: 2026-05-01
---

# Stage 7 Supervised Agency Control Plane Design

## Domain Source Of Truth

- `CONTEXT.md`
- `docs/adr/0001-canonical-authority.md`
- `docs/architecture/ADR-0001-brownfield-refactor-strategy.md`
- `docs/operations/staged-operational-proving.md`
- `docs/contracts/verification-model.md`

## Current State

Stage 7 is domain-locked as **Supervised Agency Mode**: a narrow, revocable **Agency Orchestrator** operating mode for 24/7 proof. It may create durable Odin-owned control state for eligible low-risk work, but it must preserve human approval, concurrency, kill-switch, recovery, and forbidden-path controls.

The current Odin binary already exposes `odin work ...`, but supervised dispatch is not implemented. `odin work status` reports `dispatch=not_implemented`.

Existing primitives to reuse:

- SQLite runtime authority through `internal/store/sqlite`.
- Existing `tasks` and `runs` storage-compatible backing for **Work Items** and **Run Attempts**.
- Existing GitHub intake and project registry concepts.
- Existing worktree lease, job, projection, lifecycle, and command wiring patterns.
- Existing Stage 4 through Stage 6 proof commands for worker dry-run, PR dry-run, and live docs-only PR handoff.
- Existing verification model requiring real `odin` command proof for operator-visible behavior.

## Scope

This design implements the Stage 7 control-plane proof first. It intentionally does not launch Codex workers or create pull requests.

In scope:

- `odin work supervise status`
- `odin work supervise start`
- `odin work supervise stop`
- `odin work supervise queue`
- `odin work supervise recover`
- SQLite control state for mode status, kill switch, queue decisions, dispatch claims, and recovery observations.
- Reviewed conservative config defaults with redacted config snapshot output.
- Eligibility and refusal evaluation for labels and local planned scope.
- Duplicate-dispatch prevention proof without active worker dispatch.
- Restart recovery reporting.

Out of scope:

- Codex worker launch.
- Branch or worktree creation for real task execution.
- Pull request creation or update.
- CI observation.
- Overnight live execution.
- Merge or deployment.

## Approach

Use a service-ready control plane.

The implementation should add durable control-plane services and CLI commands that future `odin serve` loops can reuse. This proves the steering and braking surfaces before any worker execution is allowed.

Rejected for this slice:

- CLI-only proof with minimal persistence, because it cannot prove restart recovery or duplicate-dispatch boundaries.
- Service-loop stub first, because hidden daemon behavior before operator controls would violate the Stage 7 trust model.

## Architecture

The Stage 7 control plane is a thin layer over existing Odin primitives:

```text
odin work supervise ...
  -> supervision control service
  -> reviewed config defaults
  -> SQLite control state
  -> issue intake facts
  -> eligibility and scope preflight
  -> queue decision records
  -> recovery observations
  -> JSON/operator reports
```

`odin serve` may call this service later, but in this slice it must not become the human control surface or start autonomous work.

Recommended package shape:

- `internal/runtime/supervision` for control-plane service, types, eligibility, refusal reasons, and recovery logic.
- `internal/store/sqlite` migration and repository methods for supervision state.
- `internal/cli/commands/work.go` for `odin work supervise ...` command wiring.
- Reviewed config extension or `config/supervision.example.yaml`, depending on the least-disruptive fit with current config loading.

## State Model

Add only the state needed for control-plane proof.

`supervision_controls`:

- current enabled or stopped state
- kill-switch state
- last operator action
- active config hash
- timestamps

`supervision_queue_decisions`:

- project key
- issue source identity
- labels observed
- eligibility result
- planned scope class
- refusal reason
- config hash
- timestamps

`supervision_dispatch_claims`:

- project key
- issue source identity
- claim status
- claim owner
- config hash
- timestamps

For this slice, claims are reserved or planned ownership records only. They are not active worker ownership.

`supervision_recovery_observations`:

- recovery status: clean, blocked, or action_required
- stale or ambiguous claims observed
- config hash comparison result
- recommended operator action
- timestamps

## Commands

`odin work supervise status --json`

Reports mode state, kill switch, config summary, queue counts, active claims, last recovery status, and explicit side-effect status.

`odin work supervise start --json`

Enables **Supervised Agency Mode** in SQLite using reviewed config defaults. It records the operator action and config hash. It does not launch workers.

`odin work supervise stop --json`

Sets the stopped or kill-switch state in SQLite and records the operator action. It does not terminate worker processes because this slice does not launch them.

`odin work supervise queue --project odin-core --json`

Evaluates candidate intake against Stage 7 labels and planned scope. It records eligible or refused queue decisions and duplicate-dispatch claims. It does not dispatch work.

`odin work supervise recover --json`

Reads prior control state, queue decisions, dispatch claims, and config hash. It records and reports clean, blocked, or action-required recovery state.

Every command report in this slice must include:

- `codex_execution=not_started`
- `prs=not_created`
- `merge=not_merged`
- `deployment=not_started`

## Eligibility And Refusal

Eligibility requires both labels:

- `odin:ready`
- `safety:low-risk`

Labels are necessary intake hints, not sufficient dispatch authority.

The local path-scope preflight may pass only when planned work is limited to:

- `docs/`
- `prompts/`
- `fixtures/`
- non-sensitive tests

Non-sensitive tests are test files outside forbidden paths and outside security, runner, workspace deletion, GitHub token, deployment, CI secret, dashboard auth, or other protected-control concerns. A `*_test.go` file inside a forbidden package, or a test that exercises protected behavior, is sensitive for Stage 7.

Stable refusal reasons:

- `missing_required_label`
- `unknown_scope`
- `forbidden_path`
- `sensitive_test_scope`
- `kill_switch_active`
- `recovery_blocked`
- `concurrency_limit_reached`

## Recovery And Duplicate Dispatch

Clean recovery means:

- no active stale claims
- no ambiguous queue state
- current config hash matches active claims

Blocked recovery means:

- stale claim exists
- prior queue decision is incomplete
- config hash changed while claims were active

Recovery must not delete worktrees, mutate GitHub, push branches, create pull requests, merge, deploy, or launch Codex. It should tell the operator what to inspect next.

A second queue evaluation for the same issue should reuse or update the existing decision record rather than creating duplicate active claims.

## Verification

Automated tests:

- Unit tests for config validation, label eligibility, path/scope classification, refusal reasons, redaction, and side-effect status.
- SQLite integration tests for controls, queue decisions, dispatch claims, and recovery observations.
- CLI tests for all `odin work supervise ...` subcommands.
- Failure tests for kill switch active, missing labels, forbidden paths, sensitive tests, unknown scope, config hash change, and duplicate queue evaluation.

Real command proof:

```bash
make build
./bin/odin work supervise status --json
./bin/odin work supervise start --json
./bin/odin work supervise queue --project odin-core --json
./bin/odin work supervise stop --json
./bin/odin work supervise recover --json
```

Each command should run against a controlled `ODIN_ROOT` during verification and prove:

- no Codex worker launch
- no GitHub write
- no pull request creation
- no merge
- no deployment
- no token exposure

## Acceptance Criteria

- Commands exist under the canonical `odin work supervise ...` surface.
- SQLite persists mutable Stage 7 control state.
- Reviewed config supplies conservative defaults.
- Queue evaluation records eligible and refused decisions.
- Duplicate-dispatch prevention is provable.
- Kill switch works.
- Restart recovery reports clean or blocked state.
- Tokens and secrets are redacted.
- No Codex worker launches.
- No GitHub writes.
- No PR creation.
- No merge.
- No deployment.

## Implementation Handoff

Start with the control-plane service and tests, then wire the CLI. Keep worker dispatch, PR creation, CI observation, and overnight proof out of this slice.

The first implementation plan should preserve these boundaries:

- no new top-level `odin agency ...` command
- no parallel database or scheduler authority
- no worker execution
- no protected-path mutation
- no GitHub write
- no deployment
