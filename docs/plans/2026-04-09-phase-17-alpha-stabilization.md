# Phase 17 Alpha Stabilization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the minimum alpha blockers from the Phase 16 reality audit so Odin OS is trustworthy for self-dogfooding and one external shadow-mode project.

**Architecture:** Keep the existing package layout and repair the missing runtime composition only. Bootstrap should initialize readiness state, queued work should run through one real executor lane, mutable execution should be gated by policy and transition rules and forced through leased worktrees, `serve` should run bounded self-heal cycles, and promoted routing refinements should affect live route selection through a small overlay rather than direct config mutation.

**Tech Stack:** Go, SQLite, embedded migrations, git worktrees, standard library process execution and timers, Go unit and integration tests

---

### Task 1: Add failing tests for fresh-runtime readiness and operational honesty

**Files:**
- Modify: `internal/app/bootstrap/bootstrap_test.go`
- Modify: `internal/runtime/health/service_test.go`
- Modify: `internal/app/lifecycle/serve_test.go`
- Modify: `tests/integration/alpha_acceptance_test.go`

**Step 1: Add fresh-runtime readiness tests**

Cover:
- bootstrap loads the registry snapshot
- bootstrap records registry freshness and projection freshness
- bootstrap records executor health samples
- `doctor` on a clean runtime can become healthy without hand-seeding

**Step 2: Add failing operational health tests**

Cover:
- `healthcheck` on a fresh runtime root succeeds after bootstrap initialization
- newline-delimited structured logs stay parseable

**Step 3: Run the focused tests**

Run: `go test ./internal/app/bootstrap ./internal/runtime/health ./internal/app/lifecycle -run 'Test(Bootstrap|Doctor|Serve|Healthcheck|Logger)'`

Expected: FAIL before implementation.

### Task 2: Implement bootstrap readiness initialization

**Files:**
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/runtime/health/service.go`
- Modify: `internal/telemetry/logs/logger.go`
- Modify: `internal/executors/router/catalog.go`
- Modify: `internal/store/sqlite/store.go`

**Step 1: Load registry and executors during bootstrap**

Add bootstrap-owned loading for:
- registry snapshot
- executor routing config
- built-in executor catalog

**Step 2: Record baseline readiness state**

Persist:
- registry version hash
- executor health samples
- baseline projection freshness surfaces

**Step 3: Fix structured log framing**

Write newline-delimited JSON records.

**Step 4: Run focused readiness tests**

Run: `go test ./internal/app/bootstrap ./internal/runtime/health ./internal/app/lifecycle`

Expected: PASS

### Task 3: Add failing tests for one real execution lane and execution-time safety

**Files:**
- Create: `internal/runtime/jobs/executor_test.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Modify: `internal/core/projects/service_test.go`
- Modify: `tests/integration/alpha_acceptance_test.go`

**Step 1: Add failing execution tests**

Cover:
- queued read-only task runs through a real executor and completes
- mutable task is denied in shadow mode
- limited-action task is denied when the action key is not allowlisted
- `odin-core` destructive or governance mutations are denied

**Step 2: Run focused execution tests**

Run: `go test ./internal/runtime/jobs ./internal/core/projects -run 'Test(Execute|Authorize|Mutation)'`

Expected: FAIL before implementation.

### Task 4: Implement the alpha execution path

**Files:**
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/executors/codex/adapter.go`
- Modify: `internal/executors/router/router.go`
- Modify: `internal/core/projects/service.go`
- Modify: `internal/store/sqlite/store.go`

**Step 1: Add one real local executor lane**

Make `codex_headless` execute deterministically for alpha and expose healthy status when local execution is available.

**Step 2: Add runtime task execution**

Implement a narrow execution method that:
- resolves the runtime project
- classifies the task as read-only or isolated mutation
- authorizes the action
- starts and finishes runs
- updates task state from queued to completed or failed

**Step 3: Add promotion-backed routing overlay**

Apply active `routing_rule_refinement` promotions when selecting executors.

**Step 4: Run focused execution tests**

Run: `go test ./internal/runtime/jobs ./internal/executors/... ./internal/core/projects`

Expected: PASS

### Task 5: Add failing tests for mandatory worktree isolation and root expansion

**Files:**
- Modify: `internal/vcs/worktrees/paths_test.go`
- Modify: `internal/vcs/leases/manager_test.go`
- Modify: `internal/runtime/jobs/executor_test.go`

**Step 1: Add failing path resolution tests**

Cover:
- `~` expands to the real home directory
- mutable execution allocates a leased worktree
- read-only execution does not allocate a mutable worktree

**Step 2: Run focused worktree tests**

Run: `go test ./internal/vcs/worktrees ./internal/vcs/leases ./internal/runtime/jobs -run 'Test(ResolvePath|Prepare|Execute)'`

Expected: FAIL before implementation.

### Task 6: Implement mandatory mutable worktree enforcement

**Files:**
- Modify: `internal/vcs/worktrees/paths.go`
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/vcs/leases/manager.go`

**Step 1: Fix worktree root expansion**

Expand `~` before path joins.

**Step 2: Route mutating tasks through leases**

Require mutable tasks to call the lease manager and use the assigned branch/worktree context before execution.

**Step 3: Run focused worktree tests**

Run: `go test ./internal/vcs/worktrees ./internal/vcs/leases ./internal/runtime/jobs`

Expected: PASS

### Task 7: Add failing tests for serve-time self-heal scheduling

**Files:**
- Modify: `internal/app/lifecycle/serve_test.go`
- Modify: `internal/runtime/recovery/service_test.go`

**Step 1: Add failing serve tests**

Cover:
- `serve` runs at least one self-heal cycle on a ticker
- the loop stops with context cancellation

**Step 2: Run focused serve tests**

Run: `go test ./internal/app/lifecycle ./internal/runtime/recovery -run 'Test(Serve|RunCycle)'`

Expected: FAIL before implementation.

### Task 8: Implement bounded self-heal scheduling in serve

**Files:**
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/runtime/recovery/service.go`
- Modify: `config/telemetry.yaml`

**Step 1: Add a bounded background self-heal loop**

Use the existing service and health config; do not add a scheduler subsystem.

**Step 2: Run focused serve tests**

Run: `go test ./internal/app/lifecycle ./internal/runtime/recovery`

Expected: PASS

### Task 9: Tighten promotion approval and runtime consumption

**Files:**
- Modify: `internal/learning/proposals/service.go`
- Modify: `internal/learning/promotion/service.go`
- Modify: `internal/learning/promotion/service_test.go`
- Modify: `internal/runtime/projections/projections.go`

**Step 1: Add a distinct promotion approval gate**

Require a post-evaluation approval status before activation.

**Step 2: Make routing promotions live**

Teach the runtime route selector to consume active routing refinements only.

**Step 3: Run focused learning and routing tests**

Run: `go test ./internal/learning/proposals ./internal/learning/promotion ./internal/executors/router ./internal/runtime/projections`

Expected: PASS

### Task 10: Add alpha readiness docs and blocker changelog, then run full verification

**Files:**
- Create: `docs/operations/alpha-readiness.md`
- Modify: `README.md`

**Step 1: Write the alpha readiness checklist**

Document:
- what is fixed
- what remains intentionally deferred
- dogfood conditions for `odin-core`
- shadow-mode conditions for one external project

**Step 2: Add a short blocker-resolution changelog**

Keep it concise in the readiness doc and README status note.

**Step 3: Run full verification**

Run:
- `make fmtcheck`
- `make lint`
- `go test ./internal/app/bootstrap ./internal/runtime/health ./internal/runtime/jobs ./internal/executors/... ./internal/vcs/... ./internal/learning/... ./internal/app/lifecycle ./internal/runtime/recovery`
- `make test-alpha`
- `make test`
- `make build`

**Step 4: Review worktree status**

Run: `git status --short --branch`

Expected: clean implementation branch with all blocker fixes verified.
