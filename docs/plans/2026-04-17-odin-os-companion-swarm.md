# Odin OS Companion Swarm Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add bounded companion swarm orchestration to `odin-os` by extending the existing workspace, companion, task, run, and delegation primitives instead of creating a second orchestration stack.

**Architecture:** Keep companions as durable role contracts and model swarms as supervised sets of delegations attached to one parent work item. Reuse existing SQLite runtime authority, approval and worktree policy, scoped memory, and CLI/status surfaces. The supervisor becomes the only component allowed to admit, create, and reconcile child delegations.

**Tech Stack:** Go, SQLite migrations and repositories, existing `internal/core/*`, `internal/runtime/*`, `internal/store/sqlite`, root `odin` CLI, standard-library JSON, current integration harness

---

### Task 1: Freeze the companion swarm contract

**Files:**
- Create: `docs/contracts/companion-swarm-orchestration.md`
- Modify: `docs/contracts/workspace-context-map.md`
- Modify: `docs/contracts/odin-operating-model.md`
- Modify: `README.md`
- Test: `docs/plans/2026-04-17-odin-os-companion-swarm-design.md`

**Step 1: Write the contract doc**

Document:

- swarm purpose and non-goals
- delegation ownership
- approval and budget boundaries
- memory view narrowing
- convergence modes
- operator visibility requirements

**Step 2: Update the existing context map**

Add explicit language that:

- companions remain durable control-plane roles
- swarms are execution patterns inside `work execution`
- `delegations` are the durable child-assignment primitive
- no second policy engine or orchestration stack may be introduced

**Step 3: Update product-facing docs**

Mention:

- companion read commands
- companion-run behavior
- swarm visibility in `status` and `agenda`

**Step 4: Run doc verification**

Run: `rg -n "companion|swarm|delegation|memory view|convergence" README.md docs/contracts docs/plans/2026-04-17-odin-os-companion-swarm-design.md`

Expected: the terms appear in one consistent vocabulary with no provider-specific or second-control-plane wording.

**Step 5: Commit**

```bash
git add README.md docs/contracts/companion-swarm-orchestration.md docs/contracts/workspace-context-map.md docs/contracts/odin-operating-model.md docs/plans/2026-04-17-odin-os-companion-swarm-design.md
git commit -m "docs: define companion swarm orchestration model"
```

### Task 2: Wire delegation persistence into the real store

**Files:**
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Create: `internal/store/sqlite/delegations_test.go`
- Test: `internal/store/sqlite/migrations/0009_delegations.sql`

**Step 1: Write the failing store tests**

Add tests for:

- creating a delegation row
- listing delegations by parent task and status
- attaching child task/run ids
- updating delegation status
- creating and listing delegation artifacts

**Step 2: Run the store tests to verify failure**

Run: `go test ./internal/store/sqlite -run 'Test(Delegation|DelegationArtifact)' -count=1 -v`

Expected: FAIL because the schema exists but store methods do not.

**Step 3: Implement the minimal store methods**

Add methods in `store.go` for:

- `CreateDelegation`
- `GetDelegation`
- `ListDelegations`
- `UpdateDelegationStatus`
- `AttachDelegationChildTask`
- `AttachDelegationWorktree`
- `CreateDelegationArtifact`
- `ListDelegationArtifacts`

Use the existing schema and model types. Do not add a new authority table.

**Step 4: Re-run the focused store tests**

Run: `go test ./internal/store/sqlite -run 'Test(Delegation|DelegationArtifact)' -count=1 -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/delegations_test.go
git commit -m "feat(store): wire delegation persistence"
```

### Task 3: Add swarm admission and planning to runtime supervision

**Files:**
- Modify: `internal/runtime/supervision/service.go`
- Create: `internal/runtime/supervision/swarm.go`
- Create: `internal/runtime/supervision/swarm_test.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/jobs/service_test.go`

**Step 1: Write the failing supervisor tests**

Add tests for:

- denying swarm creation when no valid trigger exists
- denying recursive delegation from a child task
- planning a bounded set of delegations for an eligible parent task
- narrowing child permissions relative to the parent task and companion

**Step 2: Run the supervision tests to verify failure**

Run: `go test ./internal/runtime/supervision ./internal/runtime/jobs -run 'Test(Swarm|DelegationAdmission)' -count=1 -v`

Expected: FAIL because the current supervisor only counts delayed tasks.

**Step 3: Implement admission and planning**

Add a swarm-focused layer that:

- defines admitted triggers
- resolves a bounded child-count budget
- persists delegation contracts through the store
- records child contract metadata in `details_json`
- refuses child-of-child delegation

Keep `jobs.Service` as the executor launcher. The supervisor only plans and reconciles child work.

**Step 4: Re-run the focused runtime tests**

Run: `go test ./internal/runtime/supervision ./internal/runtime/jobs -run 'Test(Swarm|DelegationAdmission)' -count=1 -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/supervision/service.go internal/runtime/supervision/swarm.go internal/runtime/supervision/swarm_test.go internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go
git commit -m "feat(runtime): add swarm admission and planning"
```

### Task 4: Materialize child work items through existing task and run primitives

**Files:**
- Modify: `internal/runtime/supervision/swarm.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/core/workitems/service.go`
- Modify: `internal/store/sqlite/store.go`
- Test: `tests/integration/companion_swarm_acceptance_test.go`

**Step 1: Write the failing integration test**

Add a new integration test that:

- bootstraps a workspace and default companion
- creates a parent task owned by that companion
- plans a small swarm
- verifies child tasks inherit workspace, initiative, and companion ownership
- verifies child tasks remain normal Odin tasks visible to projections

**Step 2: Run the focused integration test to verify failure**

Run: `go test ./tests/integration -run 'TestCompanionSwarmCreatesChildWorkItems' -count=1 -v`

Expected: FAIL because no swarm materialization path exists yet.

**Step 3: Implement child-task materialization**

Use existing task creation paths so each child:

- gets durable task ownership fields
- can run through normal queue, admission, and worktree enforcement
- can be tied back to its delegation contract

Do not add a second queue or direct executor-only child path.

**Step 4: Re-run the focused integration test**

Run: `go test ./tests/integration -run 'TestCompanionSwarmCreatesChildWorkItems' -count=1 -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/supervision/swarm.go internal/runtime/jobs/service.go internal/core/workitems/service.go internal/store/sqlite/store.go tests/integration/companion_swarm_acceptance_test.go
git commit -m "feat(runtime): materialize swarm child work items"
```

### Task 5: Add result envelopes and convergence

**Files:**
- Modify: `internal/runtime/supervision/swarm.go`
- Create: `internal/runtime/supervision/aggregate.go`
- Create: `internal/runtime/supervision/aggregate_test.go`
- Modify: `internal/store/sqlite/delegations_test.go`
- Modify: `tests/integration/companion_swarm_acceptance_test.go`

**Step 1: Write the failing aggregation tests**

Add tests for:

- merge convergence on non-overlapping child artifacts
- review-gate convergence requiring a verifier artifact
- rank convergence selecting one winning child summary
- unresolved-risk propagation into the parent state

**Step 2: Run the aggregation tests to verify failure**

Run: `go test ./internal/runtime/supervision -run 'Test(Aggregate|Convergence)' -count=1 -v`

Expected: FAIL because no aggregation logic exists.

**Step 3: Implement explicit aggregation**

Use `delegation_artifacts` as the result envelope source. Each artifact details JSON should carry:

- `status`
- `confidence`
- `evidence_refs`
- `unresolved_risks`
- `proposed_next_actions`
- `proposed_memory_candidates`

Support only:

- `merge`
- `review_gate`
- `rank`
- `quorum`

**Step 4: Re-run focused tests**

Run: `go test ./internal/runtime/supervision -run 'Test(Aggregate|Convergence)' -count=1 -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/supervision/swarm.go internal/runtime/supervision/aggregate.go internal/runtime/supervision/aggregate_test.go internal/store/sqlite/delegations_test.go tests/integration/companion_swarm_acceptance_test.go
git commit -m "feat(runtime): add swarm aggregation and result envelopes"
```

### Task 6: Surface swarm state in operator read models

**Files:**
- Modify: `internal/runtime/projections/projections.go`
- Create: `internal/runtime/projections/companion_swarm_test.go`
- Modify: `internal/runtime/conversation/service.go`
- Modify: `internal/api/http/operational.go`
- Modify: `internal/api/http/operational_test.go`
- Modify: `tests/integration/alpha_acceptance_test.go`

**Step 1: Write the failing projection tests**

Add tests for:

- active swarm summaries
- blocked swarms with approval or budget reasons
- companion-owned swarm counts in status output

**Step 2: Run the projection tests to verify failure**

Run: `go test ./internal/runtime/projections ./internal/api/http -run 'Test(CompanionSwarm|Operational)' -count=1 -v`

Expected: FAIL because projections do not query delegations today.

**Step 3: Extend projections and operator surfaces**

Add derived views built from:

- delegations
- child tasks
- child runs
- delegation artifacts

Expose them through:

- `status --json`
- `agenda --json` when companion-owned work is blocked or due
- `/healthz` and `/readyz` remain unchanged except for existing readiness semantics

Do not create a second operator API tree.

**Step 4: Re-run focused tests**

Run: `go test ./internal/runtime/projections ./internal/api/http -run 'Test(CompanionSwarm|Operational)' -count=1 -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/projections/projections.go internal/runtime/projections/companion_swarm_test.go internal/runtime/conversation/service.go internal/api/http/operational.go internal/api/http/operational_test.go tests/integration/alpha_acceptance_test.go
git commit -m "feat(operator): expose companion swarm state"
```

### Task 7: Extend the companion CLI with read surfaces

**Files:**
- Modify: `internal/cli/commands/companion.go`
- Create: `internal/cli/commands/companion_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `tests/integration/workspace_refactor_acceptance_test.go`

**Step 1: Write the failing command tests**

Add tests for:

- `odin companion get <key>`
- `odin companion state <key>`
- `odin companion capabilities <key>`
- invalid subcommand rejection still behaving cleanly

**Step 2: Run the command tests to verify failure**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'Test(ParseCompanion|RunCompanion)' -count=1 -v`

Expected: FAIL because only `create` and `list` are supported today.

**Step 3: Implement the minimal read commands**

Map them to existing runtime and companion state:

- `get`: durable companion row
- `state`: derived task and swarm state for that companion
- `capabilities`: effective tool/planning/memory policy overlays

Reuse existing workspace bootstrap and companion services.

**Step 4: Re-run focused tests**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'Test(ParseCompanion|RunCompanion)' -count=1 -v`

Expected: PASS

**Step 5: Run real Odin checks**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" go run ./cmd/odin companion create --kind advisor --key finance --title "Finance Advisor"
ODIN_ROOT="$tmp" go run ./cmd/odin companion get finance
ODIN_ROOT="$tmp" go run ./cmd/odin companion state finance --json
ODIN_ROOT="$tmp" go run ./cmd/odin companion capabilities finance --json
```

Expected:

- create succeeds
- get returns the durable row
- state and capabilities return JSON with no unsupported-subcommand failure

**Step 6: Commit**

```bash
git add internal/cli/commands/companion.go internal/cli/commands/companion_test.go internal/app/lifecycle/run.go tests/integration/workspace_refactor_acceptance_test.go
git commit -m "feat(cli): add companion read surfaces"
```

### Task 8: Add `odin companion run` on top of normal work-item creation

**Files:**
- Modify: `internal/cli/commands/companion.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Create: `tests/integration/companion_run_acceptance_test.go`

**Step 1: Write the failing run tests**

Add tests for:

- `odin companion run <key> --objective "..."`
- created task ownership by workspace, initiative, and companion
- swarm admission requested only when a supported trigger is present

**Step 2: Run the focused tests to verify failure**

Run: `go test ./internal/runtime/jobs ./internal/app/lifecycle ./tests/integration -run 'TestCompanionRun' -count=1 -v`

Expected: FAIL because `companion run` is unsupported today.

**Step 3: Implement the command**

`companion run` should:

- resolve the companion
- create a normal parent task owned by that companion
- optionally mark requested swarm trigger metadata on the task
- let the supervisor decide whether decomposition is allowed

Do not launch executor-specific subagents directly from the command.

**Step 4: Re-run focused tests**

Run: `go test ./internal/runtime/jobs ./internal/app/lifecycle ./tests/integration -run 'TestCompanionRun' -count=1 -v`

Expected: PASS

**Step 5: Run real Odin checks**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" go run ./cmd/odin companion create --kind advisor --key finance --title "Finance Advisor"
ODIN_ROOT="$tmp" go run ./cmd/odin companion run finance --objective "review April budget" --json
ODIN_ROOT="$tmp" go run ./cmd/odin status --json
```

Expected:

- run command returns a normal task and optional run summary
- status shows the companion-owned work item

**Step 6: Commit**

```bash
git add internal/cli/commands/companion.go internal/app/lifecycle/run.go internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go tests/integration/companion_run_acceptance_test.go
git commit -m "feat(cli): add companion run entrypoint"
```

### Task 9: Reconcile swarms during `serve` and harden stop conditions

**Files:**
- Modify: `internal/runtime/supervision/service.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/runtime/recovery/service.go`
- Modify: `internal/runtime/supervision/service_test.go`
- Modify: `tests/integration/alpha_acceptance_test.go`

**Step 1: Write the failing serve-loop tests**

Add tests for:

- active swarm reconciliation in the scheduler loop
- cancellation when shutdown is requested
- blocked swarm state when approval is pending
- retry budget exhaustion ending the swarm cleanly

**Step 2: Run the focused tests to verify failure**

Run: `go test ./internal/runtime/supervision ./internal/app/lifecycle ./internal/runtime/recovery -run 'Test(Swarm|ServeLoop|Shutdown)' -count=1 -v`

Expected: FAIL because serve only promotes delayed tasks today.

**Step 3: Implement bounded reconciliation**

Extend the supervision loop to:

- scan active delegations
- update derived swarm state
- queue only eligible child work
- stop on budget, deadline, approval denial, parent cancellation, or unrecoverable aggregation failure

**Step 4: Re-run focused tests**

Run: `go test ./internal/runtime/supervision ./internal/app/lifecycle ./internal/runtime/recovery -run 'Test(Swarm|ServeLoop|Shutdown)' -count=1 -v`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/supervision/service.go internal/app/lifecycle/run.go internal/runtime/recovery/service.go internal/runtime/supervision/service_test.go tests/integration/alpha_acceptance_test.go
git commit -m "feat(runtime): reconcile swarms in serve loop"
```

### Task 10: Run the full verification sweep and update ops docs

**Files:**
- Modify: `docs/operations/workspace-bootstrap.md`
- Modify: `README.md`
- Modify: `tests/integration/helpers_test.go` only if harness setup needs a companion-swarm helper

**Step 1: Update operational docs**

Document:

- how companion-run creates governed work
- when swarms are admitted
- how to inspect swarm state through `status`, `agenda`, and `companion state`

**Step 2: Run the focused Go and integration suites**

Run:

```bash
go test ./internal/store/sqlite ./internal/runtime/supervision ./internal/runtime/jobs ./internal/runtime/projections ./internal/app/lifecycle ./internal/cli/commands -count=1
go test ./tests/integration -run 'Test(WorkspaceRefactor|CompanionSwarm|CompanionRun|AlphaAcceptance)' -count=1
```

Expected: PASS

**Step 3: Run real Odin command checks**

Run:

```bash
tmp=$(mktemp -d)
ODIN_ROOT="$tmp" go run ./cmd/odin companion create --kind advisor --key finance --title "Finance Advisor"
ODIN_ROOT="$tmp" go run ./cmd/odin companion get finance
ODIN_ROOT="$tmp" go run ./cmd/odin companion state finance --json
ODIN_ROOT="$tmp" go run ./cmd/odin companion capabilities finance --json
ODIN_ROOT="$tmp" go run ./cmd/odin companion run finance --objective "review April budget" --json
ODIN_ROOT="$tmp" go run ./cmd/odin status --json
ODIN_ROOT="$tmp" go run ./cmd/odin agenda --json
```

Expected:

- all commands succeed
- no unsupported companion subcommand errors remain
- status and agenda expose companion-owned work and swarm state

**Step 4: Commit**

```bash
git add README.md docs/operations/workspace-bootstrap.md tests/integration/helpers_test.go
git commit -m "docs: finalize companion swarm operating flow"
```
