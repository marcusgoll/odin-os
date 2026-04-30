---
title: Odin Agency Orchestrator Design
status: draft
date: 2026-04-30
---

# Odin Agency Orchestrator Design

## Current State

Odin OS already owns the runtime concepts needed for an always-on software delivery agency:

- **Initiatives**, **Work Items**, **Run Attempts**, **Delivery Gates**, **Delivery Profiles**, **Feedback Loops**, and the **Delivery Workflow** in `CONTEXT.md`.
- SQLite runtime authority for projects, tasks, runs, approvals, events, context packets, worktree leases, incidents, recoveries, executor health, projections, and knowledge records.
- Managed-project enrollment through `config/projects.yaml`, including optional GitHub metadata.
- Project policy validation for worktrees, task branches, approval gates, merge policy, and destructive operations.
- Executor routing through `internal/executors/contract` and `config/executors.yaml`.
- A deterministic `codex_headless` executor seam that must be replaced or extended with a real `codex exec` runner before production agency dispatch.
- Worktree branch/path contracts under `docs/contracts/git-worktrees.md`.
- `odin serve`, `odin doctor --json`, and `odin healthcheck` as the current long-running/runtime health surface.

The missing layer is the productized multi-project delivery loop: intake from GitHub Issues, durable Work Item creation, background dispatch, real Codex execution, PR creation, QA/review agents, and human review handoff.

## What Already Exists

OpenAI Symphony is a useful reference for issue-tracker-to-agent orchestration: use the tracker as intake/control input, create one workspace per issue, keep agents running, restart crashed or stalled work, and hand results to humans for review.

Codex app-server is useful for a deeper future runner because it exposes threads, turns, streamed agent events, approvals, review mode, command execution, and generated schemas. For v1 job automation, the safer path is still a runner interface with `codex exec` first and app-server as a phase-two adapter.

## Gaps

- No implemented `odin work ...` command family in the current binary.
- No GitHub Issues intake adapter in Odin OS.
- No real `codex exec` subprocess runner behind the executor contract.
- No persistent Work Item gate table or equivalent Delivery Gate evidence projection.
- No multi-project scheduler that claims eligible work and starts workers.
- No PR manager that opens draft PRs and links them to Work Items.
- No QA/review worker lifecycle.
- No operator status surface for agency queue, active workers, PR handoffs, dry-run output, and kill-switch state.

## Reuse Plan

Reuse Odin's existing primitives instead of creating `cfipros-agency` or another parallel service:

- Use managed projects as enrolled agency targets.
- Use GitHub Issues as an **Issue Intake Source**, not runtime authority.
- Use **Work Items** as durable agency work.
- Use **Run Attempts** for planner, builder, QA, reviewer, maintainer, and triage executions.
- Use worktree leases for one mutating worker per worktree.
- Use executor routing for `codex_exec` v1 and app-server v2.
- Use approvals and events for human review, merge, and deployment boundaries.
- Use `odin serve` for background scheduling once the service loop exists.
- Use `odin work ...` as the canonical operator surface.

## New Additions

New Odin OS additions needed:

- GitHub Issues intake service.
- Delivery Gate persistence and projection.
- Agency scheduler for eligible Work Items.
- Real `codex exec` executor adapter.
- PR manager for draft PR creation and issue/PR linking.
- QA and reviewer worker roles.
- Agency status projection.
- Dry-run and kill-switch controls.
- Security policy checks for secrets, command policy, merge policy, and production deploy boundaries.

## Why New Additions Are Necessary

The current Odin codebase has the substrate, but not the always-on multi-project delivery behavior. New services are necessary because issue polling, claim/retry scheduling, Codex subprocess execution, PR handoff, and review-worker evidence are not present in the existing runtime.

These additions must be thin layers over existing Odin authority. They must not create a second database, sidecar scheduler, project-specific agency app, or GitHub-owned runtime state.

## System Architecture

The Agency Orchestrator is an Odin-owned service loop:

1. Load enrolled managed projects.
2. Poll configured Issue Intake Sources.
3. Normalize tracker records into intake facts.
4. Create or refresh Odin Work Items.
5. Select a Delivery Profile.
6. Claim eligible Work Items under concurrency and policy limits.
7. Allocate task-owned branch and worktree lease.
8. Launch a role-specific Run Attempt through the executor contract.
9. Persist structured logs, events, context packets, and artifacts.
10. Open or update a draft PR when code changes are ready.
11. Run QA and reviewer Run Attempts.
12. Produce Human Review Handoff evidence.
13. Stop before merge or production deploy.

## Component Diagram

```text
GitHub Issues
  -> Issue Intake Adapter
  -> Intake Normalizer
  -> Work Item Service
  -> Delivery Profile Selector
  -> Agency Scheduler
  -> Worktree Lease Manager
  -> Executor Router
  -> Codex Exec Runner
  -> PR Manager
  -> QA Runner
  -> Reviewer Runner
  -> Human Review Handoff
  -> odin work status / overview

SQLite Runtime Authority
  <- Work Items
  <- Run Attempts
  <- Delivery Gate Evidence
  <- Worktree Leases
  <- Approvals
  <- Events
  <- Projections
```

## Directory Structure

Target Odin OS additions:

```text
internal/core/intake/github/
internal/core/workitems/
internal/core/delivery/
internal/core/agency/
internal/runtime/agency/
internal/runtime/projections/
internal/executors/codexexec/
internal/vcs/pullrequests/
internal/cli/commands/work.go
registry/workflows/delivery-*.md
docs/contracts/agency-orchestrator.md
docs/contracts/github-intake.md
docs/contracts/delivery-gates.md
```

Do not create a new `cfipros-agency` repo for the orchestrator. `cfipros` is an enrolled managed project and may be the first proving target.

## Data Model

V1 should reuse existing tables where practical and add only the narrow missing records.

Existing tables to reuse:

- `projects`
- `tasks` as storage-compatible backing for **Work Items**
- `runs` as storage-compatible backing for **Run Attempts**
- `approvals`
- `events`
- `context_packets`
- `worktree_leases`
- `executor_health`
- projection tables

Likely new tables:

- `intake_sources`: project, provider, repo, query, enabled state, sync cursor.
- `intake_items`: provider id, issue number, labels, body hash, sync status.
- `delivery_gates`: work item, gate, status, evidence event, updated_at.
- `pull_request_links`: work item, run, provider, PR number, branch, state.
- `agency_controls`: kill switch, dry-run default, concurrency limits.

## Issue Label And State Model

GitHub labels are intake and projection, not Odin runtime truth.

Recommended labels:

- `odin:ready` eligible for intake.
- `odin:blocked` not dispatchable.
- `odin:dry-run` simulate only.
- `odin:priority-high` scheduler priority.
- `odin:needs-plan` planner first.
- `odin:running` projection from Odin active Run Attempt.
- `odin:pr-opened` projection from PR link.
- `odin:human-review` projection from Human Review Handoff.
- `odin:failed` projection from terminal or blocked agency state.

Odin should tolerate label drift and reconcile from SQLite authority.

## Agent Role Model

- Triage: classify issue and recommend Work Item shape.
- Planner: produce implementation plan or child Work Items.
- Builder: implement one Work Item in one worktree.
- QA: run tests, lint, build, and smoke checks.
- Reviewer: inspect diff, risk, missing tests, and policy violations.
- Maintainer: rebase, resolve conflicts, refresh PR metadata.

Every role execution is a **Run Attempt**.

## Prompt Template Model

Each prompt template must include:

- role and stop condition
- Work Item identity
- issue/intake evidence
- managed project identity
- repo root and worktree path
- task branch
- allowed commands
- forbidden actions
- acceptance criteria
- required structured final summary
- verification requirements
- human approval boundary

Prompt templates must not grant merge, production deploy, production secret access, or default-branch mutation authority.

## Runtime Lifecycle

Startup:

1. Open SQLite.
2. Load config and project manifests.
3. Run migrations.
4. Check kill switch and dry-run defaults.
5. Reconcile interrupted Run Attempts.
6. Refresh stale intake projections only when safe.

Dispatch:

1. Poll Issue Intake Sources.
2. Create or refresh Work Items.
3. Select Delivery Profile.
4. Claim dispatchable Work Item.
5. Allocate branch and worktree.
6. Start Run Attempt.
7. Record events and logs.
8. Advance gates only when evidence exists.

Handoff:

1. Open or update draft PR.
2. Run QA.
3. Run reviewer.
4. Record Human Review Handoff.
5. Stop before merge or deploy.

## Failure And Retry Policy

- Retry transient GitHub, network, and executor failures with exponential backoff and jitter.
- Do not retry policy denial, secret access denial, merge attempts, production deploy attempts, or default-branch mutation attempts.
- Stalled Run Attempts are interrupted and summarized before any resume.
- Resume must use Odin-owned context packets and worktree state.
- Every retry is a new Run Attempt or explicit resume event.
- Max retry exhaustion creates a human-review failure handoff.

## Security Policy

- No direct commits to default branches.
- No autonomous merge.
- No autonomous production deploy.
- No production secrets exposed to workers.
- One mutating worker uses one Work Item, one Run Attempt, one branch, and one worktree lease.
- GitHub token scopes must be minimal.
- Destructive Git commands require explicit policy allowance and approval.
- Dry-run mode must exist before unattended dispatch.
- Kill switch must stop new dispatch and interrupt or drain active workers according to policy.
- Structured logs must not include secrets.

## Deployment Plan

Use `odin serve` as the long-running service host once the agency loop lands.

Primary deployment remains systemd, matching the existing homelab operations contract:

- `deploy/systemd/odin.service`
- `deploy/systemd/odin.env.example`
- `odin healthcheck`
- `/healthz`
- `/readyz`
- `/metrics`

Docker Compose can remain secondary only after the native service model is proven.

## Implementation Phases

1. Domain and contract lock.
2. `odin work ...` minimal operator surface.
3. Delivery Gate persistence and projection.
4. GitHub Issues read-only intake.
5. Work Item creation and reconciliation.
6. Scheduler dry-run.
7. Worktree and branch dispatch.
8. Real `codex exec` runner.
9. Draft PR manager.
10. QA and reviewer Run Attempts.
11. Human Review Handoff.
12. Kill switch, rate limits, and restart recovery.
13. App-server runner behind executor contract.

## First 20 Tickets

1. Add `docs/contracts/agency-orchestrator.md`.
2. Add `docs/contracts/github-intake.md`.
3. Add `docs/contracts/delivery-gates.md`.
4. Implement `odin work profiles`.
5. Implement `odin work start` for a manual Work Item.
6. Implement Delivery Gate persistence.
7. Implement `odin work status`.
8. Implement `odin work evidence`.
9. Implement GitHub Issues read-only intake adapter.
10. Add intake source config for GitHub-backed managed projects.
11. Implement intake-to-Work-Item reconciliation.
12. Add scheduler dry-run output.
13. Add agency kill-switch state and command output.
14. Implement worktree lease dispatch for Work Items.
15. Add real `codex exec` executor adapter.
16. Persist structured Codex run logs and final summaries.
17. Open draft PRs from completed builder runs.
18. Add QA Run Attempt role.
19. Add reviewer Run Attempt role.
20. Add Human Review Handoff projection and operator output.

## Real Odin E2E Verification

Current audit proof before this design:

- `ODIN_ROOT=$(mktemp -d) ./bin/odin doctor --json` returned healthy structured JSON.
- `ODIN_ROOT=$(mktemp -d) ./bin/odin healthcheck` returned `ready`.
- `ODIN_ROOT=$(mktemp -d) bash -c "printf '/help\n/exit\n' | ./bin/odin"` rendered the live REPL help.
- `ODIN_ROOT=$(mktemp -d) ./bin/odin work status` returned `unknown command: work`.

Future implementation is incomplete until real `odin work ...` and `odin serve` command paths prove agency behavior end to end.

## Remaining Risks

- Treating GitHub labels as runtime truth instead of projections.
- Letting `cfipros` assumptions leak into the multi-project model.
- Shipping scheduler dispatch before dry-run and kill switch.
- Treating app-server protocol details as durable domain state.
- Allowing worker prompts to imply merge, deploy, or production-secret authority.

## Best Operating Rule Going Forward

GitHub Issues are project intake. Odin is runtime authority. Codex workers are executor lanes. Humans retain merge and production deploy authority.
