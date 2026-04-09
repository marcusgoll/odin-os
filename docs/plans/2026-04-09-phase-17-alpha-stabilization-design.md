# Phase 17 Alpha Stabilization Design

## Goal

Fix only the alpha blockers identified by the Phase 16 reality audit so Odin OS can be dogfooded on `odin-core` and used in shadow mode on one external project without hidden unsafe behavior.

## Scope

Phase 17 is a runtime-composition pass, not a feature phase. The work is limited to six blocker classes:

1. fresh-runtime readiness
2. one real executor lane
3. execution-time governance and transition enforcement
4. mandatory mutable worktree isolation
5. recurring self-heal inside `serve`
6. promotion-gated runtime routing changes

Anything outside those seams stays deferred.

## Current Problems

The repo already has the pieces needed for alpha, but some of them stop at contracts or isolated tests:

- a fresh `ODIN_ROOT` stays degraded because registry freshness, projection freshness, and executor health are not initialized during bootstrap
- the executor layer routes abstractly but does not execute queued work through a real lane
- project policy, transition state, and `odin-core` safety are not enforced in the runtime mutation path
- worktree leasing exists, but mutable execution is not required to use it and the default root incorrectly preserves `~` literally
- self-heal exists as a callable service but is not scheduled in the long-running runtime
- self-improvement promotions are auditable but do not require a distinct promotion approval step and do not affect live routing

## Approaches Considered

### 1. Minimal composition over the existing packages

This is the recommended approach. Keep the existing package layout, add the missing bootstrap and execution wiring, and make alpha honesty come from real runtime behavior rather than new abstractions.

### 2. New orchestration layer for jobs, executors, governance, and healing

This would add framework before the existing packages have proven they compose correctly. It solves the wrong problem for stabilization.

### 3. Test-only patching

This would improve the acceptance story without fixing the runtime truth. Phase 17 must make the implementation honest, not just the tests.

## Recommendation

Use the current package boundaries and add the minimum missing integration.

### Bootstrap real readiness state

`bootstrap.Load` should:

- load the Markdown registry snapshot from `registry/`
- record a deterministic registry version row using the compiled snapshot contents
- load executor routing config and register the built-in executor catalog
- record one executor health sample for every configured executor
- record baseline projection freshness for the operator surfaces used by doctor and metrics

The goal is that a clean runtime can become healthy without manual seeding.

### Add one real executor lane

Keep the common executor contract and declarative router, but make `codex_headless` an actual local executor for alpha. The adapter should stay simple:

- class remains `plan_backed_cli`
- health is healthy when the local environment is usable
- `RunTask` returns a deterministic structured result for local alpha work

Phase 17 does not need full external model integration. It needs one real lane that the runtime can call.

### Enforce safety at execution time

The runtime execution path must classify each task as `read_only` or `isolated_mutation` and gate it through:

- project manifest policy
- transition authorization
- `odin-core` restrictions

This enforcement belongs in the execution path, not just manifest validation tests. Shadow and compare stay read-only. Limited-action stays allowlisted only.

### Make mutable worktree isolation mandatory

Mutable execution must allocate a leased task-owned worktree and branch before the executor runs. Read-only work stays on the canonical repo root without a lease.

Phase 17 also fixes the worktree path bug by expanding `~` in the default root before joining task/run segments.

### Run self-heal inside serve

`odin serve` should start a bounded background ticker that runs `recovery.Service.RunCycle`. The loop must:

- use the existing health config
- log structured outcomes
- stop cleanly with the parent context

This is operational scheduling, not a new daemon subsystem.

### Tighten self-improvement promotion

Promotion should require a distinct post-evaluation approval state before activation. Active routing promotions should be read at runtime and applied as a lightweight overlay to the executor route config. Rollback removes the overlay.

Phase 17 only needs one live improvement lane:

- `routing_rule_refinement`

Other proposal types can keep their current audit-only lifecycle.

## Runtime Shape

Phase 17 should add a thin task executor service that composes:

- runtime store
- project registry
- transition service
- executor router
- mutable worktree lease manager

The service should:

1. select the next queued task
2. resolve project and task mutation class
3. authorize the action against project policy and transition state
4. allocate a mutable worktree when required
5. start a run
6. route and execute through the selected executor
7. finish the run and task

This is enough for alpha without building a broader scheduler.

## Testing Strategy

Phase 17 should add or repair tests around the blocker seams only:

- bootstrap readiness on a fresh runtime root
- one real execution path from queued task to completed run
- transition and policy denial during execution
- mandatory mutable worktree allocation for mutating tasks
- `serve` background self-heal activity
- promotion approval, activation, rollback, and runtime route overlay
- log newline delimiting and worktree root expansion regression coverage

The integration suite should be tightened to verify actual runtime behavior instead of isolated helper behavior where blockers were previously hidden.

## Non-Goals

Phase 17 does not include:

- additional provider integrations
- a general autonomous scheduler
- broader policy authoring changes
- new registry kinds
- broader observability polish beyond blocker fixes
- speculative orchestration abstractions
