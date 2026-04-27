# Always-On Odin Runtime Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn the current `odin serve` alpha into a minimum viable always-on control plane with explicit daemon lifecycle state, safe queueing, small scheduling, restart-safe dispatch, lease maintenance, health sampling, and an operator cutover bar.

**Architecture:** Keep one daemon process per runtime root and keep SQLite as the only runtime authority. Extend the existing `bootstrap`, `lifecycle`, `jobs`, `recovery`, `health`, `metrics`, `leases`, and `store` packages rather than adding a second service layer. Use small background loops and explicit row state to make startup, dispatch, degradation, and shutdown deterministic.

**Tech Stack:** Go, SQLite, embedded SQL migrations, standard library HTTP server, existing executor router, existing worktree lease manager, structured JSON logs, Go unit and integration tests

---

### Task 1: Add explicit daemon lifecycle state to SQLite

**Files:**
- Create: `internal/store/sqlite/migrations/0011_runtime_state.sql`
- Create: `internal/runtime/state/service.go`
- Create: `internal/runtime/state/service_test.go`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Test: `internal/runtime/state/service_test.go`
- Test: `internal/store/sqlite/store_test.go`

**Step 1: Write the failing runtime-state tests**

Add tests for:

- bootstrapping a singleton runtime-state row
- updating lifecycle state from `booting` to `ready`
- recording heartbeat time and shutdown reason

Use target assertions like:

```go
func TestRuntimeStateServiceBootAndHeartbeat(t *testing.T) {
    got, err := service.MarkBooting(ctx, BootInput{BootID: "boot-1", PID: 1234})
    if err != nil {
        t.Fatalf("MarkBooting() error = %v", err)
    }
    if got.Status != "booting" {
        t.Fatalf("Status = %q, want %q", got.Status, "booting")
    }
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/state ./internal/store/sqlite -run 'TestRuntimeState' -count=1 -v`
Expected: FAIL with missing migration, missing model, or missing service methods.

**Step 3: Add the runtime-state schema**

Create `0011_runtime_state.sql` with one singleton table:

```sql
CREATE TABLE IF NOT EXISTS runtime_state (
  singleton_key TEXT PRIMARY KEY,
  boot_id TEXT NOT NULL,
  status TEXT NOT NULL,
  pid INTEGER NOT NULL,
  started_at TEXT NOT NULL,
  ready_at TEXT,
  last_heartbeat_at TEXT NOT NULL,
  last_shutdown_reason TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
```

Use `singleton_key = 'primary'` instead of introducing multi-daemon complexity.

**Step 4: Add store methods and runtime-state service**

Implement:

- `GetRuntimeState`
- `UpsertRuntimeState`
- `UpdateRuntimeHeartbeat`

Define a model like:

```go
type RuntimeState struct {
    SingletonKey       string
    BootID             string
    Status             string
    PID                int
    StartedAt          time.Time
    ReadyAt            *time.Time
    LastHeartbeatAt    time.Time
    LastShutdownReason string
    LastError          string
    UpdatedAt          time.Time
}
```

Add service methods:

- `MarkBooting`
- `MarkRecovering`
- `MarkReady`
- `MarkDegraded`
- `MarkDraining`
- `MarkStopped`
- `Heartbeat`

**Step 5: Add auditable runtime-state events**

Extend `internal/runtime/events/events.go` with:

- `StreamService`
- `EventServiceLifecycleChanged`
- `EventServiceHeartbeatRecorded`

Keep payloads simple:

```go
type ServiceLifecyclePayload struct {
    BootID  string `json:"boot_id"`
    Status  string `json:"status"`
    Reason  string `json:"reason,omitempty"`
    PID     int    `json:"pid"`
}
```

**Step 6: Run the tests to verify they pass**

Run: `go test ./internal/runtime/state ./internal/store/sqlite -run 'TestRuntimeState' -count=1 -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/store/sqlite/migrations/0011_runtime_state.sql internal/runtime/state/service.go internal/runtime/state/service_test.go internal/runtime/events/events.go internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/store_test.go
git commit -m "feat(runtime): add explicit daemon lifecycle state"
```

### Task 2: Wire runtime-state transitions into bootstrap and serve lifecycle

**Files:**
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/serve_test.go`
- Test: `internal/app/lifecycle/serve_test.go`
- Test: `internal/app/bootstrap/bootstrap_test.go`

**Step 1: Write the failing lifecycle tests**

Add tests for:

- `serve` records `booting` before startup recovery
- `serve` records `ready` only after listener startup and loop initialization
- shutdown records `draining` then `stopped`

Use assertions like:

```go
if got.Status != "ready" {
    t.Fatalf("RuntimeState.Status = %q, want %q", got.Status, "ready")
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/bootstrap ./internal/app/lifecycle -run 'TestRunServe|TestBootstrap' -count=1 -v`
Expected: FAIL because lifecycle state is not written today.

**Step 3: Mark daemon lifecycle explicitly**

In `bootstrap.Load` or immediately after load in `runServe`:

- generate a `boot_id`
- mark runtime state `booting`
- after startup recovery completes, mark `recovering`
- after background loops and HTTP listener are live, mark `ready`
- on shutdown, mark `draining`
- before exit, mark `stopped`

Do not mark `ready` before startup recovery completes.

**Step 4: Fail closed on bootstrap or recovery errors**

If bootstrap, listener binding, or startup recovery fails:

- write `degraded` or `stopped` with `last_error`
- return the error
- do not start dispatch loops

**Step 5: Run the tests to verify they pass**

Run: `go test ./internal/app/bootstrap ./internal/app/lifecycle -run 'TestRunServe|TestBootstrap' -count=1 -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/app/bootstrap/bootstrap.go internal/app/lifecycle/run.go internal/app/lifecycle/serve_test.go internal/app/bootstrap/bootstrap_test.go
git commit -m "feat(runtime): wire daemon lifecycle state into serve"
```

### Task 3: Extend the work-item row shape for scheduling, retries, and blocking

**Files:**
- Create: `internal/store/sqlite/migrations/0012_task_queue_fields.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/runtime/projections/projections.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Test: `internal/store/sqlite/store_test.go`
- Test: `internal/runtime/jobs/service_test.go`

**Step 1: Write the failing queue-field tests**

Add tests for:

- creating a task with default queue metadata
- marking a task blocked with a reason
- setting `next_eligible_at` and retry counters

Use assertions like:

```go
if got.BlockedReason != "approval_required" {
    t.Fatalf("BlockedReason = %q, want %q", got.BlockedReason, "approval_required")
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/sqlite ./internal/runtime/jobs -run 'Test(TaskQueue|BlockedTask|RetryBackoff)' -count=1 -v`
Expected: FAIL with missing fields and store methods.

**Step 3: Add queue metadata fields**

Add these columns to `tasks`:

```sql
ALTER TABLE tasks ADD COLUMN next_eligible_at TEXT NOT NULL DEFAULT '0001-01-01T00:00:00Z';
ALTER TABLE tasks ADD COLUMN priority INTEGER NOT NULL DEFAULT 100;
ALTER TABLE tasks ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 3;
ALTER TABLE tasks ADD COLUMN blocked_reason TEXT NOT NULL DEFAULT '';
```

Keep the existing `tasks` table. Do not create a second queue table.

**Step 4: Add store helpers**

Implement helpers such as:

- `UpdateTaskQueueState`
- `BlockTask`
- `RequeueTaskAt`
- `IncrementTaskRetry`
- `ListEligibleQueuedTasks`

Use `next_eligible_at <= now` plus `status = 'queued'` as the scheduling boundary.

**Step 5: Update projections**

Expose `blocked_reason` and queue timing in blocked-item or task-status projections so operators can see why a work item is waiting.

**Step 6: Run the tests to verify they pass**

Run: `go test ./internal/store/sqlite ./internal/runtime/jobs -run 'Test(TaskQueue|BlockedTask|RetryBackoff)' -count=1 -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/store/sqlite/migrations/0012_task_queue_fields.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/runtime/projections/projections.go internal/runtime/jobs/service_test.go internal/store/sqlite/store_test.go
git commit -m "feat(queue): add scheduling and blocked-state fields"
```

### Task 4: Add a small scheduler service and loop

**Files:**
- Create: `internal/runtime/supervision/service.go`
- Create: `internal/runtime/supervision/service_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/runtime/jobs/service.go`
- Test: `internal/runtime/supervision/service_test.go`
- Test: `internal/app/lifecycle/serve_test.go`

**Step 1: Write the failing scheduler tests**

Add tests for:

- promoting a delayed queued task once `next_eligible_at` is due
- leaving not-yet-due tasks untouched
- requeueing retryable tasks with backoff

Use a small API like:

```go
result, err := service.Tick(ctx)
if err != nil {
    t.Fatalf("Tick() error = %v", err)
}
if result.Promoted != 1 {
    t.Fatalf("Promoted = %d, want 1", result.Promoted)
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/supervision ./internal/app/lifecycle -run 'TestScheduler|TestRunServe' -count=1 -v`
Expected: FAIL because supervision is currently empty.

**Step 3: Implement a minimal scheduler**

Use `internal/runtime/supervision/service.go` to:

- scan queued tasks with `next_eligible_at`
- clear stale blocked state only when an explicit unblock condition is met later
- apply retry backoff for transient failures only

Keep the first version small:

- no cron parser
- no multi-project balancing algorithm
- no independent worker pool

**Step 4: Start the scheduler loop from `runServe`**

Add a new ticker loop alongside:

- task dispatch loop
- self-heal loop

Use a separate interval such as `5s`.

**Step 5: Run the tests to verify they pass**

Run: `go test ./internal/runtime/supervision ./internal/app/lifecycle -run 'TestScheduler|TestRunServe' -count=1 -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/runtime/supervision/service.go internal/runtime/supervision/service_test.go internal/app/lifecycle/run.go internal/runtime/jobs/service.go internal/app/lifecycle/serve_test.go
git commit -m "feat(runtime): add small scheduler loop"
```

### Task 5: Refactor dispatch admission so policy, approval, and health can block safely

**Files:**
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/jobs/service_test.go`
- Modify: `internal/runtime/health/service.go`
- Modify: `internal/store/sqlite/store.go`
- Test: `internal/runtime/jobs/service_test.go`

**Step 1: Write the failing dispatch-admission tests**

Add tests for:

- degraded executor health blocks dispatch without launching a run
- missing approval blocks work and records a blocked reason
- policy or transition denial does not loop forever as queued work

Use assertions like:

```go
if got.Status != "blocked" {
    t.Fatalf("Task.Status = %q, want %q", got.Status, "blocked")
}
if got.BlockedReason != "executor_unavailable" {
    t.Fatalf("BlockedReason = %q, want %q", got.BlockedReason, "executor_unavailable")
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/jobs -run 'Test(Execute|Approval|Policy|ExecutorHealth)' -count=1 -v`
Expected: FAIL because today most failed admission paths become generic run failure or queued drift.

**Step 3: Split dispatch into admission and launch**

Inside `jobs.Service`:

- `admitTask(...)`
- `prepareLease(...)`
- `launchRun(...)`
- `finalizeOutcome(...)`

Admission must decide:

- `dispatchable`
- `blocked`
- `failed`
- `retry_later`

before any executor launch.

**Step 4: Map failures into explicit queue outcomes**

Use:

- `approval_required` -> `blocked`
- `executor_unavailable` -> `blocked` or `queued` with `next_eligible_at`
- `policy_denied` -> `failed`
- `transition_denied` -> `failed`
- `lease_conflict` -> `queued` with short retry backoff

**Step 5: Run the tests to verify they pass**

Run: `go test ./internal/runtime/jobs -run 'Test(Execute|Approval|Policy|ExecutorHealth)' -count=1 -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go internal/runtime/health/service.go internal/store/sqlite/store.go
git commit -m "refactor(dispatch): add explicit admission outcomes"
```

### Task 6: Add lease maintenance and stale-worktree cleanup

**Files:**
- Modify: `internal/vcs/leases/manager.go`
- Modify: `internal/vcs/leases/manager_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/runtime/recovery/startup.go`
- Modify: `internal/store/sqlite/store.go`
- Test: `internal/vcs/leases/manager_test.go`
- Test: `internal/app/lifecycle/serve_test.go`

**Step 1: Write the failing lease-maintenance tests**

Add tests for:

- heartbeating an active lease during a long run
- releasing completed leases
- marking stale crash-left leases for cleanup during startup recovery

Use assertions like:

```go
if got.State != "released" {
    t.Fatalf("Lease.State = %q, want %q", got.State, "released")
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vcs/leases ./internal/app/lifecycle ./internal/runtime/recovery -run 'Test(Lease|StartupRecovery)' -count=1 -v`
Expected: FAIL because there is no lease maintenance loop today.

**Step 3: Add lease maintenance helpers**

Implement helpers like:

- `HeartbeatActiveLeases`
- `ListStaleLeases`
- `CleanupReleasedOrStaleLeases`

Do not delete worktrees inside unrelated dispatch code.

**Step 4: Add a lease loop to `runServe`**

Run it on a fixed ticker. Responsibilities:

- heartbeat active leases
- retry cleanup for released or stale leases

**Step 5: Extend startup recovery**

After interrupted runs are repaired:

- identify leases belonging to interrupted runs
- mark them stale or released
- make them eligible for cleanup

**Step 6: Run the tests to verify they pass**

Run: `go test ./internal/vcs/leases ./internal/app/lifecycle ./internal/runtime/recovery -run 'Test(Lease|StartupRecovery)' -count=1 -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/vcs/leases/manager.go internal/vcs/leases/manager_test.go internal/app/lifecycle/run.go internal/runtime/recovery/startup.go internal/store/sqlite/store.go internal/app/lifecycle/serve_test.go
git commit -m "feat(vcs): add lease maintenance and stale cleanup"
```

### Task 7: Add health sampling, heartbeat refresh, and degraded daemon behavior

**Files:**
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/runtime/health/service.go`
- Modify: `internal/telemetry/metrics/service.go`
- Modify: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/api/http/operational.go`
- Test: `internal/runtime/health/service_test.go`
- Test: `internal/app/lifecycle/serve_test.go`

**Step 1: Write the failing health-loop tests**

Add tests for:

- periodic executor health sampling refreshes `executor_health`
- runtime heartbeat updates `runtime_state.last_heartbeat_at`
- degraded runtime pauses dispatch while keeping `/healthz` alive

Use assertions like:

```go
if report.Status != health.StatusDegraded {
    t.Fatalf("Report.Status = %q, want %q", report.Status, health.StatusDegraded)
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/health ./internal/app/lifecycle ./internal/api/http -run 'Test(Doctor|Serve|Readyz)' -count=1 -v`
Expected: FAIL because there is no periodic health loop or runtime-state heartbeat yet.

**Step 3: Add a health sampling loop**

In `runServe`, add a loop that:

- samples executor health from configured executors
- refreshes projection freshness for service-owned surfaces
- updates runtime-state heartbeat

Mark runtime state:

- `ready` when all required checks pass
- `degraded` when the daemon remains alive but should not dispatch new work

**Step 4: Make readiness fail closed**

Keep:

- `/healthz` for process + local inspection
- `/readyz` for dispatch-safe readiness

If runtime state is not `ready`, `/readyz` should return non-200 even if `/healthz` is still healthy enough to inspect.

**Step 5: Run the tests to verify they pass**

Run: `go test ./internal/runtime/health ./internal/app/lifecycle ./internal/api/http -run 'Test(Doctor|Serve|Readyz)' -count=1 -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/app/lifecycle/run.go internal/runtime/health/service.go internal/telemetry/metrics/service.go internal/app/bootstrap/bootstrap.go internal/api/http/operational.go internal/runtime/health/service_test.go internal/app/lifecycle/serve_test.go
git commit -m "feat(health): add runtime heartbeat and degraded dispatch behavior"
```

### Task 8: Hook memory and wake-packet writes into blocked and interrupted lifecycle paths

**Files:**
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/conversation/service.go`
- Modify: `internal/runtime/recovery/startup.go`
- Modify: `internal/memory/runs/service.go`
- Modify: `internal/memory/knowledge/service.go`
- Test: `internal/runtime/jobs/service_test.go`
- Test: `internal/runtime/recovery/startup_test.go`

**Step 1: Write the failing memory-hook tests**

Add tests for:

- approval wait creates a wake packet
- executor unavailability creates durable blocked context
- interrupted runs still produce resumable packet state

Use assertions like:

```go
packet, err := store.GetLatestTaskWakePacket(ctx, projectID, taskID)
if err != nil {
    t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
}
if packet.Trigger != "approval_wait" {
    t.Fatalf("Trigger = %q, want %q", packet.Trigger, "approval_wait")
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runtime/jobs ./internal/runtime/recovery -run 'Test(WakePacket|Approval|Restart)' -count=1 -v`
Expected: FAIL because blocked dispatch paths do not consistently create wake packets today.

**Step 3: Add explicit lifecycle memory hooks**

On:

- approval wait
- restart interruption
- executor degradation block
- dispatch failure after partial setup

write:

- a wake packet
- a short memory or transcript summary when useful

Do not write hidden prompt-only state.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/runtime/jobs ./internal/runtime/recovery -run 'Test(WakePacket|Approval|Restart)' -count=1 -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/jobs/service.go internal/runtime/conversation/service.go internal/runtime/recovery/startup.go internal/memory/runs/service.go internal/memory/knowledge/service.go internal/runtime/jobs/service_test.go internal/runtime/recovery/startup_test.go
git commit -m "feat(memory): add lifecycle wake-packet hooks"
```

### Task 9: Publish the cutover checklist and final acceptance coverage

**Files:**
- Create: `docs/operations/always-on-cutover-checklist.md`
- Modify: `tests/integration/alpha_acceptance_test.go`
- Modify: `README.md`
- Test: `tests/integration/alpha_acceptance_test.go`

**Step 1: Write the failing acceptance assertions**

Extend integration coverage for:

- healthy fresh runtime under `ODIN_ROOT`
- startup recovery on `serve`
- readiness failure when daemon is degraded
- operator visibility of blocked work and recoveries

Use a focused integration test section rather than a brand-new suite if the existing alpha acceptance test already covers the same runtime root setup.

**Step 2: Run the test to verify it fails**

Run: `go test ./tests/integration -run 'TestAlphaAcceptance' -count=1 -v`
Expected: FAIL until the always-on lifecycle behavior is fully wired.

**Step 3: Write the operator checklist**

Create `docs/operations/always-on-cutover-checklist.md` with sections for:

- install and service restart
- health and readiness verification
- startup recovery drill
- blocked approval drill
- lease cleanup drill
- backup and restore drill

**Step 4: Update the README**

Add a short “Always-On Runtime” note that points to:

- `odin serve`
- `odin healthcheck`
- `odin doctor --json`
- the new cutover checklist

**Step 5: Run the acceptance test to verify it passes**

Run: `go test ./tests/integration -run 'TestAlphaAcceptance' -count=1 -v`
Expected: PASS

**Step 6: Run the full targeted verification set**

Run: `go test ./cmd/odin ./internal/app/bootstrap ./internal/app/lifecycle ./internal/runtime/... ./internal/store/sqlite ./internal/vcs/leases ./tests/integration -count=1`
Expected: PASS

**Step 7: Commit**

```bash
git add docs/operations/always-on-cutover-checklist.md tests/integration/alpha_acceptance_test.go README.md
git commit -m "docs(runtime): add always-on cutover checklist"
```
