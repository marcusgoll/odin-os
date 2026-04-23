---
title: Odin OS Companion Swarm Orchestration
status: proposed
date: 2026-04-17
---

# Odin OS Companion Swarm Orchestration

## Existing State

`odin-os` already has the right control-plane center of gravity for this work:

- `workspace`, `initiative`, `companion`, `profile`, `followup`, and `workitem` domains are present under `internal/core`
- `tasks` and `runs` already carry `workspace_id`, `initiative_id`, `companion_id`, `blocked_reason`, `terminal_reason`, and `next_eligible_at`
- the real root CLI already supports `odin companion`, `odin initiative`, `odin profile`, `odin followup`, `odin agenda`, and `odin status`
- scoped memory already exists for workspace, initiative, companion, and run contexts
- a `delegations` and `delegation_artifacts` schema already exists in SQLite
- `internal/runtime/supervision` already exists, but today it is only a delayed-task scheduler

What is still missing is the bounded multi-agent layer on top of these primitives:

- no runtime service currently creates or reconciles delegations
- no swarm admission rules exist
- no companion-facing read surface exists for state, capabilities, or execution
- no operator read model shows swarm membership, budgets, approvals, or aggregation state

The repo therefore does not need a new product model for companions. It needs a supervised delegation model that composes with the one it already has.

## Problem

Marcus should see durable companions such as assistants, advisors, and operators, while Odin uses short-lived specialist labor behind the scenes when a goal genuinely benefits from decomposition.

The failure mode to avoid is turning every companion into a custom app or allowing unmanaged child workers to spawn recursively. Odin should remain one control plane with one policy authority, one task/run lifecycle, and one memory substrate.

## Approaches Considered

### 1. Recommended: reuse work items, runs, and delegations as the swarm substrate

Companions remain durable role contracts. A swarm is an ephemeral supervised set of delegations attached to one parent work item and parent run attempt. Child work is expressed as normal Odin tasks and runs, while `delegations` store the assignment contract and `delegation_artifacts` store structured child outputs.

Why this is the right fit:

- reuses the real runtime authority already in SQLite
- preserves the current `odin` CLI and serve loops
- keeps approvals, worktree policy, checkpoints, recovery, and memory promotion centralized
- avoids creating a second orchestration or graph engine

### 2. Add a dedicated swarm graph subsystem

Create a new swarm runtime model, planner, and read-model path separate from tasks and runs.

Why not now:

- duplicates control-plane concepts `odin-os` already has
- creates a second source of truth for execution
- increases operator confusion because work would exist outside the existing `status`, `agenda`, and `task/run` model

### 3. Make companions directly launch provider-native subagents

Let companions bypass Odin runtime objects and spawn child workers through executor-specific APIs.

Why this is wrong:

- bypasses policy and approval checkpoints
- weakens auditability and recovery
- leaks provider-specific behavior into companion semantics
- violates the repo direction that Odin owns durable truth and workers own bounded effort only

## Design Principles

1. Companion is durable, swarm is ephemeral.
2. One policy authority gates all work.
3. Child work must compile down to existing Odin tasks, runs, approvals, and memory.
4. Delegation must be purposeful, bounded, and inspectable.
5. Depth defaults to one. No recursive swarms in the first implementation.
6. Children may narrow permissions and memory visibility; they may not expand them.
7. Result aggregation must be explicit and typed, never implicit or hidden in free-form summaries.

## Core Model

### Companion

Keep the existing companion model as the user-facing role contract:

- `assistant`
- `advisor`
- `operator`
- `specialist`

Do not add a separate `monitor` kind yet. In `odin-os`, monitoring is better represented as an operating posture of `operator` or `specialist` through planning and tool-policy overlays rather than another top-level enum with duplicated lifecycle logic.

Each companion continues to own:

- charter
- initiative scope
- tool policy
- memory policy
- planning policy
- active or disabled lifecycle state

### Swarm

A swarm is not a new durable product object. It is a supervised execution pattern for one parent work item.

A swarm consists of:

- one parent task and optional parent run
- two to four child delegations by default
- zero or more child tasks and child runs
- a bounded contract describing purpose, stop conditions, and aggregation mode

Use the existing `delegations` table as the authoritative child-assignment record. Use `delegation_artifacts` as the authoritative structured output channel for child results. Store swarm-level metadata inside delegation `details_json` and derive read models from delegations plus child tasks and runs.

This keeps the system centered on existing primitives rather than introducing `swarm_instances` as a new first-class authority.

## Lifecycle

### Companion lifecycle

Companion lifecycle stays durable:

- `active`
- `disabled`
- future extension: `paused`

Companion state should remain independent of any single run.

### Swarm lifecycle

Swarm state is derived, not separately authored:

- `planned`: delegations exist, child work not yet launched
- `running`: at least one child run is active
- `blocked`: approval, policy, or budget prevents progress
- `aggregating`: child work finished and the parent is reconciling results
- `completed`: success predicate satisfied
- `failed`: unrecoverable child or parent failure
- `cancelled`: parent cancelled or shutdown requested

These states should appear in operator read models even if they are stored as derived views rather than new rows.

## Permissions And Policy

Companions must not invent their own execution logic. They express policy through existing JSON overlays plus workspace defaults.

### Permission rules

- parent task policy is the upper bound
- child delegation policy may only narrow tool access, mutation mode, branch access, and side-effect authority
- a child may never expand beyond the parent companion's effective policy
- a swarm may not bypass existing project governance, worktree, or approval rules

### Approval rules

Human approval is required when:

- the parent task already requires it
- any child requests destructive or externally visible side effects
- a swarm would cross initiative boundaries
- a swarm would exceed default child-count or budget thresholds
- a swarm proposes policy or governance mutation

No child may approve its own escalation. Approval remains external to the swarm.

### Budget rules

Every swarm must declare:

- max child count
- per-child retry budget
- total wall-clock deadline
- optional token or tool budget hints

If these are omitted, swarm creation is denied.

## Memory Views

Reuse the existing scoped memory substrate:

- workspace memory
- initiative memory
- companion memory
- run memory

The effective child memory view is resolved as:

1. workspace-visible defaults
2. initiative-visible context, if the task is initiative-owned
3. companion-visible context for the parent companion
4. parent run transcript and episodic context

Children may receive only the slices explicitly needed for their assignment. They may propose memory updates, but promotion still goes through Odin-owned write paths. No child gets its own private durable memory store.

## Delegation Rules

Delegation is allowed only through the supervisor, never directly by executor output.

### A parent may request a swarm only when all are true

- there are at least two independent or semi-independent subproblems
- the work benefits from distinct specialist roles or independent verification
- the expected result can be aggregated through an explicit convergence mode
- the parent remains the single authority for final completion

### Delegation is denied when any are true

- the work is simple enough for a single run
- decomposition is being used only to increase throughput without better outcomes
- the stop condition is undefined
- the result cannot be aggregated deterministically enough for operator review
- the task is already a child delegation

Default depth is one. Child delegations may request replanning, but they may not create more delegations directly.

## Swarm Spawn Triggers

The first implementation should support four positive triggers:

1. `parallel_research`: gather evidence or options from distinct bounded prompts
2. `build_plus_review`: one child produces, another verifies
3. `multi_artifact`: multiple independent outputs are needed for one parent result
4. `monitor_triage`: a companion must separate detection, diagnosis, and remediation recommendation

The supervisor should refuse swarm creation when none of these triggers match.

## Task Contract

Every child delegation should produce or attach:

- `delegation_key`
- parent task id and optional parent run id
- owning workspace, initiative, and companion ids
- requested role
- mutation mode
- action class and action key
- convergence mode
- artifact target
- explicit objective
- acceptance criteria
- allowed tools or capabilities subset
- effective memory view descriptor
- deadline and retry budget

The parent task remains the only object that can be marked complete for the higher-level objective.

## Result Envelope And Aggregation

Use `delegation_artifacts` as the structured child result channel.

Each child should emit at least one normalized artifact with:

- artifact type
- short summary
- structured details JSON containing:
  - status
  - evidence refs
  - confidence
  - proposed next actions
  - unresolved risks
  - proposed memory candidates

### Convergence modes

Support a small explicit set:

- `merge`: combine non-overlapping artifacts
- `review_gate`: require verifier approval of a producer child
- `rank`: compare alternatives and select one
- `quorum`: require N agreeing child outcomes before parent completion

Do not add open-ended aggregation scripting in the first version.

## Supervision Model

Expand `internal/runtime/supervision` from queue promotion into a real swarm supervisor with four responsibilities:

1. `Admit`: decide whether a swarm may be created
2. `Plan`: persist delegations and child contracts
3. `Reconcile`: observe child task/run state and move the swarm forward
4. `Aggregate`: interpret child artifacts and decide whether the parent is complete, blocked, or failed

`jobs.Service` remains the executor launcher for actual tasks. The supervisor should create and reconcile child work, not replace the jobs runtime.

`serve` should continue to own the background loop, but the loop should call both:

- queue promotion for delayed tasks
- swarm reconciliation for active delegation sets

## Operator And CLI Surfaces

Do not add a second CLI.

Extend existing surfaces instead:

- `odin status --json`: add swarm backlog, blocked swarms, and companion-owned active swarm summaries
- `odin agenda --json`: surface companion-owned blocked or due swarm work where relevant
- `odin companion list`: unchanged
- new companion read subcommands should be added later:
  - `odin companion get <key>`
  - `odin companion state <key>`
  - `odin companion capabilities <key>`
  - `odin companion run <key> --objective ...`

`odin companion run` should create a normal work item owned by the chosen companion. It may request a swarm through the supervisor if and only if the admission rules above pass.

## Safety Boundaries

- no recursive swarms
- no provider-specific companion semantics
- no direct worker-to-worker spawning
- no separate companion memory store
- no bypass of existing approval, worktree, or governance rules
- no hidden background work that lacks a durable task, delegation, or projection record

## Recommended Implementation Direction

Implement companion swarm orchestration in `odin-os` by extending:

- existing companion policy overlays
- existing task/run ownership fields
- existing `delegations` and `delegation_artifacts`
- existing serve-time supervision loop
- existing `status`, `agenda`, and `companion` CLI surfaces

Do not port the old `odin-orchestrator` CLI shape directly. `odin-os` already has the correct control-plane nouns; it needs the missing supervised delegation runtime on top of them.
