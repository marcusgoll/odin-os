---
title: Odin OS Agency Orchestrator Architecture
status: draft
date: 2026-04-30
---

# Odin OS Agency Orchestrator Architecture

## Purpose

Odin OS is a Go-native, 24/7 AI software development agency orchestrator. It reads eligible GitHub Issues for enrolled managed projects, creates isolated git worktrees, launches Codex workers, tracks progress in SQLite, opens draft pull requests, runs QA, review, and security agents, and surfaces status to a human operator.

Odin remains the runtime authority. GitHub Issues are intake and projection. Codex is an executor lane. Humans retain merge and production deployment authority.

## Current State

Odin OS already has these reusable primitives:

- Go runtime under `cmd/`, `internal/`, and `pkg`-free private packages.
- SQLite runtime authority through `internal/store/sqlite`.
- Managed project enrollment through `config/projects.yaml`.
- Worktree lease contracts and implementation under `internal/vcs`.
- Executor contract and router under `internal/executors`.
- A deterministic `codex_headless` alpha lane that must become or be replaced by a real `codex exec` runner before production agency dispatch.
- Runtime events, projections, health, recovery, `odin serve`, `odin doctor --json`, and `odin healthcheck`.
- Delivery Workflow language in `CONTEXT.md`: Work Item, Run Attempt, Delivery Gate, Delivery Profile, Issue Intake Source, Agency Orchestrator, and Human Review Handoff.

The requested `cmd/odin-os/main.go` service entrypoint is a target packaging choice. The current repo-owned command path is `cmd/odin/main.go` and the operator surface is `odin`. Implementation planning must decide whether to add a second `odin-os` daemon binary or keep one `odin` binary with `odin serve` as the daemon mode.

## System Architecture

The agency loop runs as a long-running Go daemon:

1. Load Odin configuration and enrolled managed projects.
2. Poll configured GitHub Issue intake sources.
3. Normalize eligible issues into Odin Work Items.
4. Select a Delivery Profile.
5. Claim dispatchable Work Items under concurrency, policy, dry-run, and kill-switch controls.
6. Allocate one task-owned branch and one active mutable worktree lease for each mutating Run Attempt.
7. Render a role-specific prompt from Odin-owned prompt templates.
8. Launch Codex CLI through a `codex exec` runner behind the executor interface.
9. Persist structured logs, events, context packets, artifacts, runner metadata, and Run Attempt state.
10. Open or update draft pull requests.
11. Run QA, review, and security Run Attempts.
12. Produce a Human Review Handoff.
13. Stop before merge or production deployment.

## Component Diagram

```text
GitHub Issues
  -> GitHub Intake Adapter
  -> Intake Normalizer
  -> Work Item Service
  -> Delivery Profile Selector
  -> Agency Scheduler
  -> Worktree Lease Manager
  -> Executor Router
  -> Codex Exec Runner
  -> Pull Request Manager
  -> QA Runner
  -> Review Runner
  -> Security Runner
  -> Human Review Handoff
  -> Dashboard / API / odin work status

SQLite Runtime Authority
  <- Projects
  <- Work Items
  <- Run Attempts
  <- Delivery Gate Evidence
  <- Worktree Leases
  <- Approvals
  <- Pull Request Links
  <- Events
  <- Projections
  <- Recovery Records
```

Future runner:

```text
Executor Router
  -> Codex App-Server Runner
  -> thread/start
  -> turn/start
  -> streaming item events
  -> review/start
```

The app-server runner must remain behind the same executor interface. App-server thread and turn identifiers are runner metadata, not durable Work Item authority.

## Go-Native Directory Structure

Target additions:

```text
cmd/odin-os/main.go
internal/agency/
  scheduler/
  service/
  controls/
internal/core/intake/github/
internal/core/workitems/
internal/core/delivery/
internal/dashboard/http/
internal/executors/codexexec/
internal/executors/codexappserver/
internal/prompts/
internal/review/
internal/security/
internal/vcs/pullrequests/
registry/workflows/delivery-*.md
docs/contracts/agency-orchestrator.md
docs/contracts/github-intake.md
docs/contracts/delivery-gates.md
```

Existing package boundaries still apply:

- `internal/core`: intake, routing, approvals, policy, scheduling, and project governance.
- `internal/runtime`: runs, events, projections, health, recovery, and checkpoints.
- `internal/executors`: model-agnostic executor contracts and runner adapters.
- `internal/vcs`: Git, branches, pull requests, worktrees, and leases.
- `internal/api/http`: dashboard/API transport over shared runtime services.
- `internal/app/lifecycle`: binary lifecycle, `serve`, healthcheck, and startup recovery.

## Data Model

Reuse existing tables where possible:

- `projects`: enrolled managed projects.
- `tasks`: storage-compatible backing for Work Items until schema promotion.
- `runs`: storage-compatible backing for Run Attempts.
- `approvals`: human decisions and approval boundaries.
- `events`: append-only runtime event stream.
- `context_packets`: resumable handoff and wake context.
- `worktree_leases`: branch and worktree allocation.
- `executor_health`: runner health.
- `incidents` and `recoveries`: failure and startup recovery records.
- projection tables: operator-readable state.

Likely new tables:

- `intake_sources`: project, provider, repository, query, enabled state, sync cursor.
- `intake_items`: provider id, issue number, labels, body hash, sync status.
- `delivery_gates`: Work Item, gate, status, evidence event, updated_at.
- `pull_request_links`: Work Item, Run Attempt, provider, PR number, branch, state.
- `agency_controls`: kill switch, dry-run default, concurrency limits.
- `worker_processes`: runner pid, uid, cwd, sandbox mode, started_at, heartbeat_at.

## Issue Label And State Model

GitHub labels are intake and projection. SQLite remains runtime truth.

Input labels:

- `odin:ready`
- `odin:blocked`
- `odin:dry-run`
- `odin:priority-high`
- `odin:needs-plan`
- `odin:security-review`

Runtime projection labels:

- `odin:claimed`
- `odin:running`
- `odin:pr-opened`
- `odin:qa-running`
- `odin:review-running`
- `odin:security-running`
- `odin:human-review`
- `odin:failed`
- `odin:stalled`

Terminal projection labels:

- `odin:done`
- `odin:abandoned`
- `odin:needs-human`

Odin must tolerate label drift and reconcile from SQLite authority.

## Agent Role Model

- Triage: classify issue readiness and recommend Work Item shape.
- Planner: produce implementation plan or child Work Items.
- Builder: implement one Work Item in one worktree.
- QA: run tests, lint, build, and smoke checks.
- Reviewer: inspect diff, risks, missing tests, policy violations, and handoff quality.
- Security: inspect secret handling, command policy, dependency risk, and deployment boundary violations.
- Maintainer: rebase, resolve conflicts, refresh PR metadata, and update stale handoff state.

Every role execution is a Run Attempt.

## Prompt Template Model

Prompt templates are authored assets under `prompts/` or `registry/workflows/` and rendered by Go services.

Every prompt must include:

- role and stop condition
- Work Item identity
- intake evidence
- managed project identity
- repo root and worktree path
- task branch
- allowed commands
- forbidden actions
- acceptance criteria
- required structured final summary
- verification requirements
- human approval boundary
- explicit prohibition on root execution, danger-full-access Codex mode, production secret access, autonomous merge, and autonomous deploy

## Runtime Lifecycle

Startup:

1. Open SQLite.
2. Load config, project manifests, registry entries, and prompt assets.
3. Run migrations.
4. Check kill switch and dry-run defaults.
5. Verify the daemon is not running workers as root.
6. Reconcile interrupted Run Attempts.
7. Reconcile active worktree leases.
8. Refresh stale intake projections only when safe.

Dispatch:

1. Poll issue intake sources.
2. Create or refresh Work Items.
3. Select Delivery Profiles.
4. Claim dispatchable Work Items.
5. Allocate branch and worktree.
6. Start role-specific Run Attempt through the executor router.
7. Record events, context packets, process metadata, and structured logs.
8. Advance Delivery Gates only when Odin-owned evidence exists.

Handoff:

1. Open or update draft PR.
2. Run QA.
3. Run review.
4. Run security.
5. Record Human Review Handoff.
6. Wait for human merge, rejection, follow-up, or deployment decision.

Shutdown:

1. Stop polling.
2. Drain or interrupt active workers according to policy.
3. Persist interruption context.
4. Preserve worktrees for review unless cleanup policy explicitly releases them.

## Failure And Retry Policy

- Retry transient GitHub, network, and executor failures with exponential backoff and jitter.
- Do not retry policy denial, root-worker denial, danger-full-access denial, secret access denial, merge attempts, production deploy attempts, or default-branch mutation attempts.
- Stalled Run Attempts are interrupted and summarized before any resume.
- Resume must use Odin-owned context packets and worktree state.
- Every retry is a new Run Attempt or explicit resume event.
- Max retry exhaustion creates a human-review failure handoff.
- Startup recovery must record every interrupted, resumed, abandoned, or escalated run.

## Security Policy

Security rules are defined in `docs/SECURITY.md`. Architectural baseline:

- no direct commits to main or default branches
- no autonomous merge
- no autonomous production deploy
- no production secrets in worker context
- one mutating worker maps to one issue-derived Work Item and one worktree
- workers must not run as root
- Codex must not run in danger-full-access mode
- dry-run mode exists before unattended dispatch
- kill switch exists before unattended dispatch
- logs are structured and redact secrets

## Deployment Plan

Primary deployment:

- Go binary built from the repo.
- `systemd` user or system service running as a non-root service user.
- SQLite on persistent disk.
- structured logs to stdout and runtime log files.
- Go HTTP server for `/healthz`, `/readyz`, `/metrics`, and dashboard/API status.
- `odin healthcheck` or equivalent machine-oriented readiness check.

Secondary deployment:

- Docker Compose only if the container runs as a non-root user and mounts config, state, worktree, and log volumes explicitly.

## Verification Requirements

Implementation is incomplete until real Odin command paths prove behavior:

- `odin work ...` for Work Item state, evidence, queue, dry-run, and handoff.
- `odin serve` or `odin-os` daemon mode for polling, scheduling, recovery, and health.
- `odin doctor --json` and `odin healthcheck` for runtime readiness.

Internal package tests are not sufficient proof for operator-visible agency behavior.
