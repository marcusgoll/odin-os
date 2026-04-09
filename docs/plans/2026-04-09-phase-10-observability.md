# Phase 10 Observability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add structured logs, typed health and doctor checks, metrics export, incident and recovery operator surfaces, and freshness-aware projections so operators can see what is broken, degraded, blocked, or stale.

**Architecture:** Extend SQLite with projection freshness tracking, keep observability state derived from canonical runtime data, and add shared services in `internal/telemetry`, `internal/runtime/health`, and `internal/runtime/projections`. The CLI `/doctor` command becomes a renderer over a structured doctor report rather than a one-line string.

**Tech Stack:** Go, SQLite via `database/sql`, embedded SQL migrations, JSON output, Go tests

---

### Task 1: Lock structured telemetry contracts with failing tests

**Files:**
- Create: `docs/contracts/observability.md`
- Create: `internal/telemetry/logs/logger_test.go`
- Create: `internal/telemetry/metrics/service_test.go`

**Step 1: Write the failing structured logging test**

Cover:
- JSON line output
- correlation identifier fields
- task, run, and scope metadata

**Step 2: Write the failing metrics export test**

Cover:
- metrics snapshot contains counts for active runs, blocked items, approvals waiting, incidents, recoveries, and freshness
- text export is machine-parseable

**Step 3: Run the focused telemetry tests and verify they fail**

Run: `go test ./internal/telemetry/logs ./internal/telemetry/metrics`
Expected: FAIL because the telemetry packages are empty.

**Step 4: Implement the minimal logging and metrics contracts**

Add the structured logger and metrics snapshot plus export formatter only to the extent required by the tests.

**Step 5: Re-run the focused telemetry tests**

Run: `go test ./internal/telemetry/logs ./internal/telemetry/metrics`
Expected: PASS

### Task 2: Add projection freshness storage with failing tests

**Files:**
- Create: `internal/store/sqlite/migrations/0004_projection_freshness.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Create: `internal/store/sqlite/projection_freshness_test.go`

**Step 1: Write the failing projection freshness tests**

Cover:
- recording or upserting projection freshness
- listing freshness rows
- stale selection behavior through timestamps and statuses

**Step 2: Run the focused store freshness tests and verify they fail**

Run: `go test ./internal/store/sqlite -run 'TestProjectionFreshness'`
Expected: FAIL because the migration and store methods do not exist yet.

**Step 3: Implement the migration and store methods**

Add the minimum schema and helpers needed to support health and metrics checks.

**Step 4: Re-run the focused store freshness tests**

Run: `go test ./internal/store/sqlite -run 'TestProjectionFreshness'`
Expected: PASS

### Task 3: Build the doctor and health service with failing tests

**Files:**
- Modify: `internal/runtime/health/service.go`
- Modify: `internal/runtime/health/service_test.go`
- Create: `internal/runtime/health/doctor_test.go`

**Step 1: Write the failing doctor report tests**

Cover:
- healthy state
- degraded state for registry issues, queue pressure, stale executors, and stale sources
- failed state for DB failure
- machine-readable doctor report structure

**Step 2: Run the focused health tests and verify they fail**

Run: `go test ./internal/runtime/health`
Expected: FAIL because the current health summary is too thin.

**Step 3: Implement the minimal doctor and health checks**

Add:
- component checks
- overall doctor report
- thresholds and default config
- DB, registry, executor, queue, projection, and source freshness checks

**Step 4: Re-run the focused health tests**

Run: `go test ./internal/runtime/health`
Expected: PASS

### Task 4: Add operator projections and incident surfaces with failing tests

**Files:**
- Modify: `internal/runtime/projections/projections.go`
- Create: `internal/runtime/projections/observability_test.go`

**Step 1: Write the failing projection tests**

Cover:
- active runs
- blocked items
- approvals waiting
- incidents
- recoveries
- freshness
- project portfolio view

**Step 2: Run the focused projection tests and verify they fail**

Run: `go test ./internal/runtime/projections`
Expected: FAIL because the new projection views do not exist yet.

**Step 3: Implement the minimal read-only projection helpers**

Keep these as derived views over SQLite and wake-packet payload parsing, without adding mutable side effects.

**Step 4: Re-run the focused projection tests**

Run: `go test ./internal/runtime/projections`
Expected: PASS

### Task 5: Add the CLI doctor surface and full verification

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `README.md`
- Modify: `config/telemetry.yaml`

**Step 1: Write the failing CLI doctor tests**

Cover:
- text output shows component states
- `/doctor json` returns machine-parseable JSON
- healthy, degraded, and failed states are distinguishable

**Step 2: Run the focused CLI tests and verify they fail**

Run: `go test ./internal/cli/repl`
Expected: FAIL because `/doctor` does not render structured output yet.

**Step 3: Implement the minimal CLI doctor rendering**

Use the shared doctor service rather than duplicating logic in the shell.

**Step 4: Run focused verification**

Run: `go test ./internal/telemetry/... ./internal/store/sqlite ./internal/runtime/health ./internal/runtime/projections ./internal/cli/repl`
Expected: PASS

**Step 5: Run full verification**

Run: `make fmtcheck && make lint && make test && make build`
Expected: PASS

**Step 6: Commit**

```bash
git add README.md config/telemetry.yaml docs/contracts/observability.md docs/plans/2026-04-09-phase-10-observability.md internal/cli/repl/shell.go internal/cli/repl/shell_test.go internal/runtime/health/service.go internal/runtime/health/service_test.go internal/runtime/health/doctor_test.go internal/runtime/projections/projections.go internal/runtime/projections/observability_test.go internal/store/sqlite/migrations/0004_projection_freshness.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/projection_freshness_test.go internal/telemetry/logs internal/telemetry/metrics
git commit -m "feat: add observability and doctor surfaces for phase 10"
```
