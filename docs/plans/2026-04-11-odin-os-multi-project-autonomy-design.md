---
title: Odin OS Multi-Project Autonomy Design
status: proposed
date: 2026-04-11
---

# Odin OS Multi-Project Autonomy Design

## Goal

Make `odin-os` the primary controller for normal unattended work across multiple projects while keeping human approval for high-risk actions:

- destructive operations
- governance and policy changes
- system-project mutation
- any project action outside its transition allowlist

The target is not "alpha that looks busy." The target is a production-grade controller that can accept, route, execute, recover, explain, and safely complete real work without leaning on `odin-orchestrator` for day-to-day execution.

## Current Reality

`odin-os` is now the intended runtime root, but it is still an alpha composition pass rather than the live operating system:

- the Phase 16 audit says the main gap is composition drift, not missing package scaffolding
- the repo has one live `codex_headless` alpha lane and explicit deferrals around broader scheduler behavior and unattended orchestration
- the local `odin-os` runtime has only a handful of tasks and failed runs
- the real machine workload still flows through the legacy `/var/odin` stack

That means the plan must close execution truth, not add more architecture prose.

## Scope

This design covers the minimum system needed for full multi-project autonomy with human-gated high-risk actions:

1. honest bootstrap and health state
2. one production-grade harness executor lane
3. durable task and run lifecycle
4. execution-time policy, transition, and approval enforcement
5. mandatory leased worktrees for mutable work
6. runtime-backed tools and bounded actions
7. long-running service behavior, self-heal, restart recovery, and operator-visible logs
8. multi-project queue control, budgets, concurrency, and stuck-run handling
9. dual-run cutover from `odin-orchestrator`

This design does not include:

- fully autonomous destructive or governance mutations
- multiple production executor providers on day one
- a big-bang cutover
- retirement of the legacy stack before `odin-os` proves completion quality in production

## Approaches

### 1. Progressive dual-run cutover

Keep `odin-orchestrator` available as fallback while `odin-os` takes over capabilities and then projects in measured stages.

Pros:

- validates runtime truth under real workload
- gives a clean rollback path
- exposes scheduler, approval, and recovery gaps before full cutover

Cons:

- requires duplicate monitoring during transition
- forces explicit ownership boundaries between the two systems

### 2. Big-bang replacement

Finish the missing features in `odin-os`, stop the legacy stack, and cut over everything at once.

Pros:

- shortest migration narrative
- no temporary dual-control complexity

Cons:

- highest outage risk
- hides which subsystem actually failed when production traffic lands
- does not fit the current executor and operator-surface maturity

### 3. Extended shadow alpha

Leave `odin-os` in shadow mode for an extended period and keep improving internals before promoting it.

Pros:

- safest technically
- easy to keep iterating

Cons:

- creates permanent "almost ready" drift
- delays learning from real autonomous execution

## Recommendation

Use progressive dual-run cutover.

The system should graduate through explicit gates instead of phase labels:

1. `truthful alpha`
2. `single-project live autonomy`
3. `multi-project shadow control`
4. `multi-project primary control`
5. `legacy retirement`

Each gate must be earned with runtime evidence, not package-level test coverage alone.

## Target Architecture

### 1. Runtime authority stays in `odin-os`

`odin-os` keeps SQLite, project manifests, transition state, approvals, checkpoints, worktree leases, and run metadata as the system of record. Legacy `/var/odin` files are migration inputs or temporary compatibility outputs only during cutover.

### 2. One real harness lane becomes production-grade

The first production executor lane should be the harness-based Codex or Claude path already being introduced in the CLI cutover work. It must be able to:

- claim a queued task
- materialize execution context
- launch a real harness process
- stream structured progress
- capture artifacts and transcript summaries
- finish, retry, cancel, or resume cleanly

Production autonomy does not require multiple providers. It requires one real lane that survives real work.

### 3. Execution-time safety is the hard boundary

Every mutating run must be classified before execution and must pass:

- project manifest policy
- transition authorization
- action allowlisting for `limited_action`
- system-project approval gates
- destructive-operation approval gates
- leased task worktree allocation

If any of those checks fail, the task must stop before the executor launches.

### 4. The control loop must be explicit

`odin-os` needs a single composed runtime loop that:

1. samples health and queue state
2. selects runnable tasks subject to project and budget limits
3. starts runs with full policy context
4. routes to tools and executors
5. records structured progress and uncertainty
6. recovers interrupted work
7. requests approvals for blocked high-risk actions
8. self-heals bounded failures

This is the difference between a library collection and an operating system.

### 5. Operator visibility must explain the system

The operator surface must answer, in text and JSON:

- what is queued, running, blocked, failed, and stalled
- which project owns each task
- why a task is blocked
- which approvals are waiting
- which budgets or concurrency caps are in effect
- what the last self-heal actions did
- whether `odin-os` or legacy currently owns mutation authority for each project

Without that, multi-project autonomy will drift into silent queue buildup.

## Rollout Gates

### Gate 0: Acceptance contract

Define the production exit bar in docs and tests:

- fresh runtime becomes healthy without seeding
- one executor lane completes real durable work
- mutable work always uses leased worktrees
- high-risk work always requests approval
- service restart resumes interrupted work
- multi-project scheduling respects per-project limits and global budgets

### Gate 1: Truthful alpha

Required evidence:

- `make test-alpha`, `make test`, and `make build` pass
- fresh `ODIN_ROOT` reaches healthy state
- structured logs are newline-delimited and operator-visible
- `odin serve` exits cleanly on signal and restarts cleanly

### Gate 2: Single-project live autonomy

Required evidence:

- one non-system project runs real unattended normal work end-to-end
- approvals stop high-risk actions
- restart recovery and self-heal handle at least one injected failure mode
- completion quality is good enough to keep the project in `limited_action` or `cutover`

### Gate 3: Multi-project shadow control

Required evidence:

- at least three projects are registered
- `odin-os` can observe and simulate queue decisions across them
- budgets, concurrency, and stuck-run rules prevent starvation
- project reports capture shadow observations and compare results

### Gate 4: Multi-project primary control

Required evidence:

- `odin-os` becomes the primary normal-work controller for pilot projects
- legacy stack is no longer required for routine completion on those projects
- approval backlog, failure rate, and stall rate stay within agreed thresholds

### Gate 5: Legacy retirement

Required evidence:

- pilot projects stay stable under `odin-os` primary control
- legacy services are reduced to rollback-only
- the remaining `/var/odin` responsibilities are either migrated or explicitly archived

## Operational Policy

### Approval model

Human approval remains mandatory for:

- system-project mutation
- destructive git or filesystem actions
- project-governance changes
- any bounded action marked high risk

Normal issue, task, and bounded-action execution should remain unattended when the project transition state allows it.

### Scheduler model

The scheduler should be conservative by default:

- global concurrency cap
- per-project concurrency cap
- per-project budget ceiling
- bounded retry counts
- explicit stall detection that demotes or dead-letters stuck runs

### Cutover order

Projects should move in this sequence:

1. `odin-core` remains the policy reference and operator proving ground
2. one external project in `shadow`
3. one external project in `limited_action`
4. one external project in `cutover`
5. additional projects only after the first cutover project stays stable

## Success Metrics

The cutover is only real when these hold for live pilot projects:

- last successful completion is recent, not historical
- queue depth is bounded
- dead-letter rate is low and explained
- approval wait times are visible
- stalled runs are recovered or failed deterministically
- operator can inspect a project and explain current control state in under five minutes

## Risks

The main risks are:

- proving route selection without proving execution
- leaving policy enforcement in validation code instead of runtime code
- building a scheduler before the run lifecycle is trustworthy
- declaring cutover before operator visibility is good enough
- trying to retire legacy before `odin-os` has live completion history

## Design Summary

This is a controlled systems migration. The winning strategy is:

- make `odin-os` truthful
- make one executor lane real
- enforce safety at the runtime boundary
- add explicit multi-project control and operator visibility
- cut over normal work gradually
- keep approval gates for high-risk actions
- retire legacy only after `odin-os` proves it can run the work, not just describe it
