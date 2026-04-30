---
title: Odin OS Agency Orchestrator Roadmap
status: draft
date: 2026-04-30
---

# Odin OS Agency Orchestrator Roadmap

## Implementation Phases

### Phase 1: Domain And Contracts

Lock the Agency Orchestrator as a Go-native Odin-owned multi-project Delivery Workflow capability.

Deliverables:

- `docs/contracts/agency-orchestrator.md`
- `docs/contracts/github-intake.md`
- `docs/contracts/delivery-gates.md`
- updated Delivery Workflow registry entries
- explicit decision on `cmd/odin-os/main.go` versus existing `cmd/odin/main.go` plus `odin serve`

### Phase 2: Minimal `odin work ...`

Expose the canonical operator surface before unattended dispatch.

Deliverables:

- `odin work profiles`
- `odin work start`
- `odin work status`
- `odin work evidence`
- focused command-level E2E proof

### Phase 3: Delivery Gate State

Persist gate status and evidence for Work Items.

Deliverables:

- Delivery Gate storage or compatible event/projection model
- gate ordering validation
- missing-evidence errors
- projection output for current gate and next action

### Phase 4: GitHub Issue Intake

Read GitHub Issues as intake sources for GitHub-backed managed projects.

Deliverables:

- standard-library-first GitHub HTTP adapter
- intake source config
- normalized intake records
- label and state eligibility rules
- no mutation in this phase

### Phase 5: Scheduler Dry Run

Decide what would run without creating branches, worktrees, comments, labels, PRs, or workers.

Deliverables:

- dry-run scheduler
- concurrency decision output
- blocked reason output
- kill-switch awareness
- root-worker and danger-full-access policy checks

### Phase 6: Workspace Dispatch

Create task-owned branches and worktrees for claimed Work Items.

Deliverables:

- branch naming integration
- worktree lease allocation
- lease recovery
- cleanup policy
- one mutating worker per active mutable lease

### Phase 7: Codex Exec Runner

Run Codex through a real Go `codex exec` executor adapter.

Deliverables:

- `codex_exec` executor behind the shared contract
- subprocess launch with context cancellation
- non-root execution check
- explicit denial of danger-full-access mode
- structured stdout/stderr capture
- final summary parsing
- timeout and cancellation behavior
- context packet resume support

### Phase 8: Pull Request Handoff

Open and update draft PRs from completed builder work.

Deliverables:

- PR creation adapter
- issue-to-PR linkage
- draft PR default
- branch and run metadata
- no merge support

### Phase 9: QA, Review, And Security Roles

Run QA, review, and security Run Attempts after builder output.

Deliverables:

- QA role prompt and runner
- reviewer role prompt and runner
- security role prompt and runner
- artifact persistence
- failure handoff policy
- findings projection

### Phase 10: Human Review Handoff

Record the point where Odin waits for human merge, rejection, follow-up, or deployment decision.

Deliverables:

- handoff event
- handoff projection
- PR, QA, review, and security evidence summary
- follow-up Work Item creation path
- explicit non-approval language

### Phase 11: Dashboard And API

Expose status through the Go HTTP server without creating a second control plane.

Deliverables:

- `/api/agency/status`
- `/api/agency/work-items`
- `/api/agency/runs`
- `/api/agency/handoffs`
- dashboard-ready JSON projections
- read-only by default

### Phase 12: Service Hardening

Make the orchestrator safe to run continuously.

Deliverables:

- `odin serve` or `odin-os` daemon agency loop
- startup recovery
- rate limits
- kill switch
- dry-run enforcement
- backup coverage
- structured metrics
- systemd unit running as non-root

### Phase 13: Codex App-Server Runner

Add a deeper Codex integration behind the executor interface after v1 is stable.

Deliverables:

- app-server runner adapter
- generated schema pinning
- thread and turn metadata mapping
- streaming event persistence
- review mode integration
- fallback to `codex_exec`

## First 20 Tickets

1. Add `docs/contracts/agency-orchestrator.md`.
2. Add `docs/contracts/github-intake.md`.
3. Add `docs/contracts/delivery-gates.md`.
4. Decide binary entrypoint: add `cmd/odin-os/main.go` or keep `cmd/odin/main.go` with `odin serve`.
5. Implement `odin work profiles`.
6. Implement `odin work start` for a manual Work Item.
7. Implement Delivery Gate persistence.
8. Implement `odin work status`.
9. Implement `odin work evidence`.
10. Implement GitHub Issues read-only intake adapter.
11. Add intake source config for GitHub-backed managed projects.
12. Implement intake-to-Work-Item reconciliation.
13. Add scheduler dry-run output.
14. Add agency kill-switch state and operator output.
15. Implement root-worker and danger-full-access policy checks.
16. Implement worktree lease dispatch for Work Items.
17. Add real Go `codex exec` executor adapter.
18. Persist structured Codex run logs and final summaries.
19. Open draft PRs from completed builder runs.
20. Add QA, review, security, and Human Review Handoff projections.

## Acceptance Gates

Each phase must include:

- focused unit or contract tests for new logic
- integration tests for SQLite, Git, subprocesses, or runtime services when applicable
- real `odin` command proof for operator-visible behavior
- failure-path proof for at least one meaningful error
- explicit Proven and Unproven summary

## Deferred Work

- autonomous merge
- autonomous production deploy
- production secret access for workers
- running workers as root
- danger-full-access Codex mode
- multi-tenant SaaS control plane
- replacing Odin runtime authority with GitHub state
- exposing Codex app-server details as durable Work Item state
