---
title: Always-On Odin Runtime Design
status: proposed
date: 2026-04-16
---

# Always-On Odin Runtime Design

## Goal

Define the minimum viable Odin runtime that can run continuously in the background and reliably own system state before Odin becomes highly autonomous.

Success means:

- one clear control plane
- one clear state authority
- deterministic startup, shutdown, restart, and crash recovery behavior
- safe queueing and dispatch for a solo operator
- observable health, incidents, and blocked work
- explicit hooks for policy, memory, and future scheduling

## Existing Repo State

The current `odin-os` repo already has most of the raw ingredients for an always-on runtime:

- `cmd/odin` supports `serve`, `healthcheck`, `doctor`, backup, restore, and verify-backup flows
- `internal/app/bootstrap` creates the runtime root, opens SQLite, runs migrations, loads registry and project manifests, and seeds readiness rows
- `internal/app/bootstrap/lock.go` already prevents concurrent bootstrap for the same runtime root
- `internal/app/lifecycle/run.go` already has a service mode with an HTTP operational server, a queued-task loop, and a self-heal loop
- `internal/runtime/recovery/startup.go` already converts `running` runs into interrupted work and creates restart wake packets
- `internal/runtime/health`, `internal/telemetry/metrics`, and `internal/runtime/projections` already provide machine-readable health and operator views
- `internal/runtime/jobs/service.go` already owns queued-task execution, executor routing, transition checks, and worktree lease allocation
- `internal/store/sqlite` already records canonical state plus append-only runtime events in the same transaction
- `internal/memory/*` and `context_packets` already provide restart-safe memory and handoff primitives

What is still missing is not a new platform. It is an explicit control-plane contract that says:

- which loop owns which responsibility
- which state transitions are allowed
- how work is admitted, delayed, blocked, retried, or failed
- when the daemon is allowed to dispatch work
- how Odin behaves when dependencies are degraded or a process dies mid-run

## Approaches Considered

### 1. Single daemon with in-process control loops and subprocess workers

This is the recommended approach.

The `odin` process remains the only control-plane daemon. It owns queue scanning, schedule promotion, policy admission, health sampling, recovery, and worker dispatch. Actual work still executes through executor adapters and external subprocesses or APIs.

Pros:

- matches the existing `serve` model
- keeps ownership simple
- restart behavior is easy to reason about
- fits SQLite and a solo-operator operating model

Cons:

- dispatch concurrency stays intentionally low
- one bad in-process bug can still stop the daemon, so restart and recovery must be strong

### 2. Split scheduler/service and separate worker services

Rejected for the minimum viable cutover.

Pros:

- clearer CPU isolation
- easier future scale-out

Cons:

- duplicates lifecycle logic too early
- requires distributed leases or service-to-service coordination
- overcomplicates failure handling for a single-node SQLite system

### 3. Scripted always-on mode around CLI commands and cron

Rejected.

Pros:

- fast to assemble

Cons:

- no clear state owner
- poor restart semantics
- no coherent queue and policy admission layer
- too much invisible process behavior

## Recommendation

Use one long-lived `odin serve` daemon as the control plane.

Keep these boundaries:

- SQLite owns runtime truth
- the daemon owns lifecycle and dispatch
- executor adapters own external worker launches
- worker processes own only one run attempt at a time
- registry, config, and durable authored memory remain Git-authored inputs, not mutable runtime truth

The runtime should start small:

- global dispatch concurrency: `1`
- per-project concurrency: `1`
- queue and scheduling stored in the existing work item table shape
- explicit restart recovery instead of trying to resume in-memory processes

## Runtime Principles

### 1. One runtime authority

`data/odin.db` remains the only canonical runtime authority.

### 2. One control plane

There is exactly one daemon process per runtime root.

### 3. Fail closed

If bootstrap, recovery, policy validation, or core health checks cannot complete safely, Odin stays unready and does not dispatch work.

### 4. No invisible magic

Every important state change must be visible in:

- SQLite rows
- append-only runtime events
- operator projections or logs

### 5. Synchronous dispatch first

Minimum viable dispatch should run one work item at a time by default. Increase concurrency only after service behavior is trustworthy.

## Control Plane Components

### 1. Bootstrap Loader

Implemented base:

- `internal/app/bootstrap`
- `internal/app/bootstrap/lock.go`

Responsibilities:

- create runtime directories
- acquire the bootstrap lock
- open SQLite
- run migrations
- load registry and manifest inputs
- load executor config
- seed readiness records needed by doctor and metrics

### 2. Runtime State Manager

New minimum component.

Purpose:

Record the daemon’s own lifecycle explicitly instead of leaving it implied by process existence.

Recommended storage:

- add a singleton `runtime_state` row
- optionally add a `service` stream type in runtime events

Minimum fields:

- `boot_id`
- `status` with values `booting`, `recovering`, `ready`, `degraded`, `draining`, `stopped`
- `pid`
- `started_at`
- `ready_at`
- `last_heartbeat_at`
- `last_shutdown_reason`
- `last_error`

### 3. Queue Manager

Base implementation:

- `internal/runtime/jobs/service.go`

Responsibilities:

- list eligible queued work
- atomically claim the next work item
- reject unsafe work before launch
- mark items `queued`, `running`, `blocked`, `completed`, `failed`, or `canceled`

Important rule:

Do not create a second queue subsystem. The existing work-item row shape remains the queue.

Recommended minimum queue fields added to the current `tasks` model:

- `next_eligible_at`
- `priority`
- `last_error`
- `retry_count`
- `max_attempts`
- `blocked_reason`

### 4. Scheduler Loop

New minimum component, but intentionally small.

Responsibilities:

- promote delayed work into the active queue when `next_eligible_at <= now`
- apply retry backoff after transient failures
- optionally materialize simple routine-driven work later

Explicit non-goal:

Do not add a general cron engine or distributed scheduler for the first always-on cutover.

### 5. Policy Admission Gate

Base implementation already exists across:

- project manifest policy
- transition authorization
- worktree lease rules

Responsibilities:

- determine whether the daemon may dispatch the work item at all
- decide whether approval is required before dispatch
- reject forbidden mutations
- distinguish retriable infrastructure failure from hard policy denial

Dispatch rule:

No executor is launched until policy admission succeeds.

### 6. Worker Dispatcher

Base implementation:

- `internal/runtime/jobs/service.go`
- `internal/executors/*`

Responsibilities:

- choose an execution lane through the executor router
- construct the task spec
- allocate required worktree lease
- launch the external worker
- finalize the run attempt and resulting work item state

Minimum viable dispatch behavior:

- one active run at a time per daemon
- worker launch is synchronous from the dispatcher goroutine
- HTTP health and background monitoring stay alive while the worker runs

### 7. Memory Hooks

Base implementation pieces:

- `internal/memory/*`
- `conversation_transcripts`
- `memory_summaries`
- `context_packets`

Responsibilities:

- persist transcripts and episode memory on run completion or failure
- create wake packets on restart, approval wait, and handoff
- record enough durable context for safe human or automated resume

Minimum rule:

Memory hooks must be explicit lifecycle writes, not hidden background prompt tricks.

### 8. Recovery Manager

Base implementation:

- `internal/runtime/recovery/startup.go`
- `internal/runtime/recovery/service.go`

Responsibilities:

- startup recovery for interrupted runs
- bounded self-heal cycles for known runtime faults
- incident and recovery record creation
- explicit escalation when automation should stop

### 9. Health and Observability Manager

Base implementation:

- `internal/runtime/health`
- `internal/telemetry/metrics`
- `internal/runtime/projections`
- structured logs under `runs/logs`

Responsibilities:

- readiness evaluation
- health reporting
- metrics snapshots
- freshness tracking
- operator-visible blocked work, incidents, approvals, and recoveries

### 10. Operational HTTP Surface

Base implementation:

- `internal/api/http/operational.go`

Responsibilities:

- `/healthz`
- `/readyz`
- `/metrics`

Important rule:

This is an operational surface for the daemon, not a second control plane.

## Service Lifecycle

### Runtime states

The daemon should move through these explicit phases:

1. `booting`
2. `recovering`
3. `ready`
4. `degraded`
5. `draining`
6. `stopped`

`ready` means:

- SQLite is open
- migrations are complete
- registry and manifests loaded
- startup recovery completed
- required executor health is acceptable
- operational HTTP listener is bound

`degraded` means:

- the daemon is still alive and observable
- dispatch is paused for unsafe work
- self-heal and operator inspection remain available

## Startup Flow

1. Start `odin serve`.
2. Resolve `ODIN_ROOT`, config, and repo root.
3. Acquire bootstrap lock for the runtime root.
4. Open SQLite and run migrations.
5. Load registry snapshot, project manifests, executor config, and durable session inputs.
6. Write `runtime_state = booting`.
7. Seed or refresh registry version, executor health rows, and projection freshness rows.
8. Run startup recovery:
   - find `runs.status = running`
   - mark them `interrupted`
   - move affected work items back to `queued` or `blocked` as appropriate
   - create restart wake packets
   - record recovery actions and events
   - release or mark stale worktree leases for cleanup
9. Start the operational HTTP server.
10. Start background loops:
   - dispatch loop
   - scheduler loop
   - self-heal loop
   - health sampling and freshness refresh loop
   - lease heartbeat and cleanup loop
11. Write `runtime_state = ready`.
12. `/readyz` returns healthy only after step 11 completes.

## Shutdown Flow

1. Receive SIGTERM or explicit stop request.
2. Write `runtime_state = draining`.
3. Stop admitting new work.
4. Keep `/healthz` up during drain; `/readyz` should fail closed.
5. Allow the active worker a bounded shutdown window:
   - if it exits cleanly, finalize the run normally
   - if it does not exit within the timeout, cancel it if supported, then terminate the daemon
6. Flush structured logs.
7. Release worktree leases that are safe to release.
8. Write `runtime_state = stopped` with shutdown reason.
9. Exit so the service manager can restart only when needed.

Important rule:

If the daemon dies unexpectedly before step 8, startup recovery is responsible for repairing the partial runtime state.

## Background Jobs

### 1. Dispatch loop

Interval:

- fast poll, currently about `1s`

Responsibilities:

- if runtime state is `ready`, find the next eligible queued item
- check project policy, transitions, approvals, executor health, and worktree requirements
- dispatch exactly one run at a time by default

### 2. Scheduler loop

Interval:

- `5s` to `15s`

Responsibilities:

- move delayed work into dispatchable state
- apply retry backoff expiration
- promote due operator or routine work later through the same queue

### 3. Self-heal loop

Interval:

- currently about `30s`

Responsibilities:

- observe runtime faults
- execute bounded playbooks
- escalate incidents after retry limits

### 4. Health sampling and freshness loop

Interval:

- `30s` to `60s`

Responsibilities:

- resample executor health
- refresh projection freshness rows
- update daemon heartbeat in `runtime_state`

### 5. Lease maintenance loop

Interval:

- `30s`

Responsibilities:

- heartbeat active mutable worktree leases
- release completed leases
- clean stale crash-left worktrees after startup recovery or timeout

## Queueing and Scheduling Model

### Work item state machine

Minimum states:

- `queued`
- `running`
- `blocked`
- `completed`
- `failed`
- `canceled`

### Blocked reasons

Explicit reasons should include at least:

- `approval_required`
- `executor_unavailable`
- `policy_denied`
- `transition_denied`
- `lease_conflict`
- `dependency_degraded`

### Scheduling rule

The scheduler should use `next_eligible_at` on the work item itself.

That means:

- no second queue table
- no hidden retry queue
- no cron-only engine

Later recurring schedules should materialize normal queued work items instead of inventing a separate execution path.

## Worker Dispatch Rules

### Admission checks before launch

Every dispatch attempt must pass, in order:

1. runtime state is `ready`
2. registry and manifest inputs are valid
3. required executor health is acceptable
4. work item is due and not blocked
5. project transition allows the action
6. project policy allows the action
7. required approval has already been granted
8. mutable work gets a valid worktree lease
9. executor routing succeeds

### Dispatch output contract

Each attempt must leave behind:

- updated work item state
- updated run attempt state
- append-only events
- transcript and memory records when applicable
- wake packet if the work is paused, blocked, or interrupted

## Failure Handling

### Daemon crash

Expected behavior:

- OS service manager restarts the process
- bootstrap lock prevents overlapping startup
- startup recovery handles `running` runs left behind
- interrupted work is requeued or blocked with a restart wake packet

### Executor unavailable

Expected behavior:

- readiness degrades
- dispatcher stops launching new runs that require the executor
- queued work stays queued with `next_eligible_at` backoff or explicit blocked reason
- operator can inspect the degraded condition through doctor and metrics

### Policy or transition denial

Expected behavior:

- do not retry automatically
- mark the work item `blocked` or `failed` with explicit reason
- emit denial event
- create approval or review wake packet when appropriate

### Approval wait

Expected behavior:

- create approval row
- set work item to `blocked`
- create wake packet with next action
- never keep a worker process alive waiting for a human indefinitely

### Partial persistence failure

Expected behavior:

- if a transactional state write fails, the dispatch step fails as a whole
- no side-effecting worker launch should occur after a failed admission write
- if failure happens after external launch, mark incident and rely on startup recovery plus operator review

### Worktree cleanup failure

Expected behavior:

- release the logical lease only when safe
- if filesystem cleanup fails, mark the lease stale and let the maintenance loop retry
- surface the cleanup problem through incident or warning logs

## Storage Responsibilities

### SQLite: canonical runtime authority

Owns:

- projects
- work items via current `tasks`
- run attempts via current `runs`
- approvals
- incidents
- recoveries
- runtime events
- context packets
- executor health
- registry versions
- projection freshness
- worktree leases
- conversation transcripts
- memory summaries
- runtime state singleton

### Git-authored repo inputs

Own:

- registry definitions
- prompts
- durable authored memory files
- config and manifests

These are inputs to the control plane, not mutable runtime authority.

### Filesystem-derived outputs

Own:

- service logs
- caches
- rendered summaries
- temporary worktrees

These are disposable or reconstructable relative to SQLite and authored inputs.

## Minimum Always-On Cutover Checklist

### Service lifecycle

- `odin serve` starts cleanly under a dedicated runtime root
- the daemon records explicit runtime state transitions
- shutdown drains cleanly and marks the runtime stopped

### Restart safety

- killing the daemon mid-run leaves auditable interrupted state
- restart recovery requeues or blocks interrupted work safely
- restart wake packets are created for every interrupted run

### Queue and dispatch

- one queued work item can run end-to-end without manual SQL edits
- policy-denied work is blocked or failed explicitly, not silently dropped
- approval-required work pauses before side effects

### Worktree safety

- every mutable run acquires a task-owned worktree lease
- completed runs release or mark leases for cleanup
- stale leases are recoverable after restart

### Observability

- `/healthz`, `/readyz`, and `/metrics` are live
- `odin healthcheck` fails closed when runtime is unsafe
- `odin doctor --json` explains degraded dependencies
- service logs are newline-delimited JSON

### Memory and handoff

- run completion writes transcript and memory summaries
- approval wait and restart create wake packets
- resumed work can proceed from durable packet state without depending on raw in-memory session history

### Backup and operator safety

- backup, restore, and verify-backup all work against the runtime root
- one restore drill has been completed before production cutover

## Explicit Non-Goals

This minimum design does not require:

- multiple always-on daemon roles
- distributed workers
- HA SQLite
- general cron scheduling
- automatic resurrection of live model sessions
- high concurrency by default
- autonomous governance or destructive mutations

## Recommendation Summary

The minimum viable persistent Odin control plane is one daemon process running `odin serve`, backed by SQLite, with explicit runtime states, one queue, one small scheduler, one dispatcher, one recovery loop, and one operational HTTP surface.

That design is:

- resilient enough for crashes and reboots
- observable enough for a solo operator
- simple enough to cut over safely
- extensible enough to grow later into broader scheduling, supervision, and swarms without replacing the core runtime model
