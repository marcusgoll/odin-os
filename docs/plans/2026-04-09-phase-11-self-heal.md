# Phase 11 Self-Heal Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add deterministic self-heal monitors, diagnosis rules, bounded recovery playbooks, and escalation behavior that write auditable runtime records and operator-visible projections.

**Architecture:** Keep self-heal logic code-defined in `internal/runtime/recovery`, derive observations from existing health and projection state, and persist all actions through the SQLite store and runtime event model. Extend the event and store layers only where needed for explicit escalation and recovery-action auditability.

**Tech Stack:** Go, SQLite via `database/sql`, existing runtime events/projections/health packages, structured logging

---

### Task 1: Add the self-heal contract and failing event/store tests

**Files:**
- Create: `docs/contracts/self-heal.md`
- Modify: `internal/runtime/events/events.go`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Test: `internal/store/sqlite/self_heal_test.go`

**Step 1: Write the failing store and event tests**

Add tests that prove Phase 11 can:

- record a recovery action event with playbook, fault key, action name, and attempt
- escalate an incident with an explicit escalation reason
- persist recovery completion states including `escalated`

**Step 2: Run the focused test to verify it fails**

Run: `go test ./internal/store/sqlite -run 'TestSelfHeal'`
Expected: FAIL because the new event types and store methods do not exist yet.

**Step 3: Implement the minimal event and store changes**

Add:

- new runtime event types for recovery action execution and incident escalation
- typed payloads in `internal/runtime/events/events.go`
- minimal new store methods in `internal/store/sqlite/store.go`
- any supporting model structs in `internal/store/sqlite/models.go`
- the self-heal contract doc in `docs/contracts/self-heal.md`

**Step 4: Run the focused test to verify it passes**

Run: `go test ./internal/store/sqlite -run 'TestSelfHeal'`
Expected: PASS

**Step 5: Commit**

```bash
git add docs/contracts/self-heal.md internal/runtime/events/events.go internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/self_heal_test.go
git commit -m "feat: add self-heal event and store contract"
```

### Task 2: Add monitors and diagnosis with failing tests

**Files:**
- Create: `internal/runtime/recovery/types.go`
- Create: `internal/runtime/recovery/monitors.go`
- Create: `internal/runtime/recovery/diagnosis.go`
- Test: `internal/runtime/recovery/monitors_test.go`
- Test: `internal/runtime/recovery/diagnosis_test.go`

**Step 1: Write the failing monitor and diagnosis tests**

Cover:

- stale executor health produces `executor_health_stale`
- stale projection freshness produces `projection_stale`
- stale registry freshness produces `source_freshness_stale`
- queue pressure above threshold produces `queue_pressure_high`
- repeated failed runs for one task produce `run_failure_repeated`
- unknown observations do not map to a playbook

**Step 2: Run the focused tests to verify they fail**

Run: `go test ./internal/runtime/recovery -run 'TestMonitor|TestDiagnos'`
Expected: FAIL because the recovery package and monitor logic do not exist yet.

**Step 3: Implement the minimal monitor and diagnosis logic**

Add typed observations, fault keys, and diagnosis rules that return explicit playbook selections or no-op decisions.

**Step 4: Run the focused tests to verify they pass**

Run: `go test ./internal/runtime/recovery -run 'TestMonitor|TestDiagnos'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/recovery/types.go internal/runtime/recovery/monitors.go internal/runtime/recovery/diagnosis.go internal/runtime/recovery/monitors_test.go internal/runtime/recovery/diagnosis_test.go
git commit -m "feat: add self-heal monitors and diagnosis"
```

### Task 3: Add bounded playbook execution with failing tests

**Files:**
- Create: `internal/runtime/recovery/playbooks.go`
- Create: `internal/runtime/recovery/executor.go`
- Test: `internal/runtime/recovery/executor_test.go`

**Step 1: Write the failing executor tests**

Cover:

- cooldown suppresses a repeat playbook run
- retries stop at the configured limit
- repeated failure escalates instead of retrying forever
- successful action records a recovery action event and completes the recovery

**Step 2: Run the focused test to verify it fails**

Run: `go test ./internal/runtime/recovery -run 'TestExecutor'`
Expected: FAIL because no playbook executor exists yet.

**Step 3: Implement the minimal executor and playbooks**

Implement:

- typed playbook definitions
- bounded retry and cooldown checks
- explicit action execution against the store
- escalation path when retry limits are exhausted

Use deterministic actions only. Do not add provider or policy mutation behavior.

**Step 4: Run the focused test to verify it passes**

Run: `go test ./internal/runtime/recovery -run 'TestExecutor'`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/recovery/playbooks.go internal/runtime/recovery/executor.go internal/runtime/recovery/executor_test.go
git commit -m "feat: add bounded self-heal playbook execution"
```

### Task 4: Add service integration, logging, and projection coverage with failing tests

**Files:**
- Create: `internal/runtime/recovery/service.go`
- Modify: `internal/runtime/projections/projections.go`
- Modify: `internal/runtime/projections/observability_test.go`
- Modify: `internal/telemetry/metrics/service.go`
- Modify: `internal/telemetry/metrics/service_test.go`
- Test: `internal/runtime/recovery/service_test.go`

**Step 1: Write the failing integration tests**

Cover:

- a self-heal cycle opens or reuses an incident, starts a recovery, and records the action
- escalated incidents and recoveries show up in projections
- metrics reflect escalated items or active recoveries after a self-heal cycle

**Step 2: Run the focused tests to verify they fail**

Run: `go test ./internal/runtime/recovery ./internal/runtime/projections ./internal/telemetry/metrics`
Expected: FAIL because the integrated cycle and projection updates do not exist yet.

**Step 3: Implement the minimal integration**

Add a service that:

- runs monitors
- applies diagnosis
- executes one bounded playbook per fault key
- writes structured logs

Extend projections and metrics only where Phase 11 needs additional visibility, such as escalated incident counts or latest self-heal outcomes.

**Step 4: Run the focused tests to verify they pass**

Run: `go test ./internal/runtime/recovery ./internal/runtime/projections ./internal/telemetry/metrics`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/recovery/service.go internal/runtime/recovery/service_test.go internal/runtime/projections/projections.go internal/runtime/projections/observability_test.go internal/telemetry/metrics/service.go internal/telemetry/metrics/service_test.go
git commit -m "feat: integrate self-heal with projections and metrics"
```

### Task 5: Add docs, README updates, and final verification

**Files:**
- Modify: `README.md`
- Modify: `config/telemetry.yaml`
- Modify: `internal/runtime/health/service.go`
- Modify: `internal/cli/repl/shell.go` only if `/doctor` needs minimal self-heal status wording

**Step 1: Add any final failing tests if doctor or health output changes**

Only add focused tests if the self-heal changes require doctor output changes.

**Step 2: Implement the minimal documentation and surface updates**

Update:

- `README.md` to Phase 11
- telemetry thresholds or knobs only if the new self-heal service needs them
- doctor or health summary only if the new escalation state needs to be exposed there

**Step 3: Run focused verification**

Run: `go test ./internal/runtime/recovery ./internal/store/sqlite ./internal/runtime/projections ./internal/telemetry/metrics ./internal/runtime/health`
Expected: PASS

**Step 4: Run full verification**

Run: `make fmtcheck && make lint && make test && make build`
Expected: exit 0

**Step 5: Commit**

```bash
git add README.md config/telemetry.yaml internal/runtime/health/service.go internal/cli/repl/shell.go docs/contracts/self-heal.md internal/runtime/recovery internal/runtime/projections internal/telemetry/metrics internal/store/sqlite internal/runtime/events
git commit -m "feat: add deterministic self-heal playbooks for phase 11"
```
