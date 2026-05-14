---
title: Work Execution Orchestration Architecture Review
status: draft
date: 2026-05-14
mode: audit-first
---

# Work Execution Orchestration Architecture Review

## Objective

Assess the current **Work Execution** orchestration shape and preserve the
current depth of `jobs.Service` while identifying the next safe direction for
narrowing interfaces.

This is a candidate review only. It does not propose a replacement module or a
new interface shape.

## Success Criteria

- Confirm the current module shape around `internal/app/lifecycle/run.go`,
  `internal/runtime/jobs/service.go`, and `internal/runtime/runs/service.go`.
- Verify the existing **Work Item** / **Run Attempt** contract is already
  durable and should not be replaced.
- Apply the deletion test to `jobs.Service`.
- Classify dependencies for future interface narrowing.
- Identify the next safe refactor direction without broad rewrite risk.

## Context Read

- `CONTEXT.md` defines **Work Item** as the durable unit of governed work and
  **Run Attempt** as one concrete execution attempt for a Work Item.
- `docs/contracts/work-execution-state.md` defines the state ownership and
  command proof matrix for Work Items and Run Attempts.
- `docs/contracts/executor-contract.md` defines executor classes and the
  `TaskSpec` contract at the executor adapter seam.
- `docs/architecture/ADR-0001-brownfield-refactor-strategy.md` requires
  brownfield refactors to preserve working modules, avoid duplicate systems,
  and add characterization tests before risky behavior changes.
- `docs/superpowers/specs/2026-05-10-jobs-runs-work-execution-design.md`
  already locks the first Work Execution slice around existing `jobs.Service`,
  `runs.Service`, SQLite `tasks` / `runs`, `odin work`, `odin jobs`, and
  `odin runs`.

## Current Module Shape

`internal/app/lifecycle/run.go` remains the broad lifecycle and operator
dispatch module. It wires top-level commands, serve loops, readiness loops,
HTTP handlers, background task dispatch, and runtime services. The file is
approximately 7,001 lines in the current checkout.

`internal/runtime/jobs/service.go` is the deep Work Execution module. It is
approximately 3,244 lines and owns:

- Work Item creation and intent backfill.
- queue admission and policy blocking.
- dispatch and execution orchestration.
- executor routing and `TaskSpec` construction.
- worktree lease preparation.
- Run Attempt preparation and finalization.
- retry policy and failed-work recovery guidance.
- execution evidence and memory recording.

`internal/runtime/runs/service.go` is a smaller Run Attempt readback and helper
module. It is approximately 477 lines and owns Run Attempt listing, show,
envelope readback, and direct start / complete / fail helpers. Its `Start`
helper starts a run and moves a queued task to running, but the primary
dispatch and execution path still lives in `jobs.Service`.

## Observed Friction

The **Work Item** / **Run Attempt** contract is solid, but orchestration
leverage is diluted across large modules:

- `run.go` constructs `jobs.Service` in multiple command and serve paths.
- `run.go` owns background loop sequencing and calls into Work Execution from
  lifecycle helpers such as `attemptDispatchIfReady`.
- `commands.RunWork` exposes the operator surface and delegates to
  `jobs.Service`, but it still constructs a default `jobs.Service` in command
  helpers when no injected service is provided.
- `jobs.Service` contains multiple paths that repeat the same conceptual
  phases: load Work Item, load Managed Project, resolve manifest, select
  executor, admit policy, prepare Run Attempt, prepare lease, construct
  `TaskSpec`, execute, finalize, and record evidence.
- `runs.Service` has direct start helpers, but the richer governed dispatch
  semantics are correctly retained in `jobs.Service`.

This is real complexity, not just a naming issue. The risk is that future
workflow-specific slices keep adding lifecycle wiring or command-specific
knowledge instead of narrowing the Work Execution interface.

## Deletion Test

Deleting `jobs.Service` would not remove complexity. It would push these
responsibilities into lifecycle, CLI command helpers, supervisor modules, and
executor adapters:

- policy admission
- approval blocking
- executor selection
- worktree lease orchestration
- checkpoint and wake-packet handling
- Run Attempt state transitions
- failure analysis
- retry and recovery guidance
- execution artifact recording

Result: `jobs.Service` earns its depth. The correct future direction is to
narrow what callers must know about Work Execution, not replace the module.

## Dependency Category

Mixed:

- **Local-substitutable** for SQLite-backed Work Item / Run Attempt state,
  projections, events, approvals, and lease records. Existing tests can use a
  local SQLite store and test adapters.
- **True external** at executor adapter boundaries. Harness-driver executors,
  browser workers, GitHub-backed effects, and subprocess lanes must stay behind
  injected executor or adapter seams.

Future tests should cross the Work Execution interface with local SQLite state
and fake executor adapters. They should not require live executor subprocesses
unless the slice explicitly proves a real lane.

## Candidate 3 - Work Execution Orchestration

- Files:
  - `internal/app/lifecycle/run.go`
  - `internal/cli/commands/work.go`
  - `internal/runtime/jobs/service.go`
  - `internal/runtime/runs/service.go`
  - `docs/contracts/work-execution-state.md`
  - `docs/contracts/executor-contract.md`
- Current shape:
  - Lifecycle owns top-level command and serve-loop wiring.
  - `commands.RunWork` owns the `odin work` operator surface.
  - `jobs.Service` owns governed Work Execution behavior.
  - `runs.Service` owns Run Attempt readback and direct helper methods.
- Friction:
  - Callers still need to know too much about how to construct `jobs.Service`
    with registry, executors, prompt renderer, transitions, leases, runtime
    root, and clock.
  - `jobs.Service` contains several orchestration paths with overlapping phase
    structure.
  - Lifecycle remains large enough that adding more Work Execution wiring there
    increases navigation cost and regression risk.
- Deletion test:
  - Strong keep. Deleting `jobs.Service` spreads policy, leases, executors,
    checkpoints, Run Attempt transitions, retry, and recovery into callers.
- Dependency category:
  - Mixed: local-substitutable for SQLite/state tests; true external at
    executor adapter boundaries.
- Proposed deepening:
  - Keep `jobs.Service` as the Work Execution behavior owner.
  - Narrow construction and caller knowledge around Work Execution before
    moving behavior.
  - Prefer a small lifecycle-side assembly helper or existing bootstrap-owned
    factory over a new orchestration module.
  - After construction is narrowed, characterize repeated dispatch / execute
    phases inside `jobs.Service` and extract only private implementation seams
    that do not leak into callers.
- Testing impact:
  - Start with characterization tests around `odin work dispatch`,
    `odin work execute`, `odin jobs`, and `odin runs` using local SQLite and
    fake executors.
  - Keep executor adapters faked unless the slice explicitly targets a live
    lane.
- Benefits:
  - Leverage: callers get Work Execution behavior through one narrower setup
    path instead of recreating the service shape.
  - Locality: policy, lease, executor, retry, and recovery changes remain in
    Work Execution instead of leaking into lifecycle or CLI helpers.
- Risks:
  - Broad extraction could accidentally create a parallel orchestrator.
  - Moving behavior before characterization would risk Work Item / Run Attempt
    state regressions.
  - Directly reshaping executor seams touches security-sensitive process and
    adapter behavior.
- Recommendation strength:
  - Medium. The friction is real, but the safe next step is interface
    narrowing and characterization, not a behavior move.

## What Not To Change Yet

- Do not replace `jobs.Service`.
- Do not introduce a new Work Execution queue, workflow-run table, executor
  framework, or prompt-agent runtime authority.
- Do not rename physical `tasks` or `runs` storage.
- Do not move executor launch behavior without a security review.
- Do not design a new public interface until a specific caller-friction slice
  is selected.

## Next Decision

Choose one narrow follow-up slice:

1. Centralize `jobs.Service` construction for lifecycle and `odin work`
   command paths.
2. Characterize and reduce duplication between dispatch-only and
   execute-after-dispatch paths inside `jobs.Service`.
3. Clarify whether `runs.Service.Start` should remain a direct helper or become
   private to governed Work Execution paths.
