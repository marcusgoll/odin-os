# Phase 13 Project Transition Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an auditable project transition ladder that enforces read-only, limited-action, and cutover authority for managed projects without allowing dual-controller mutation.

**Architecture:** Store current transition state and append-only transition reports in SQLite, extend the runtime event model for transition changes and reports, and add one reusable authorization gate that combines transition state, controller ownership, requested action class, and existing project governance policy. Keep shadow and compare read-only, limited-action narrow, and cutover explicit.

**Tech Stack:** Go, SQLite, embedded SQL migrations, existing runtime events and projections, Go unit tests

---

### Task 1: Add the transition contract docs

**Files:**
- Create: `docs/contracts/project-transition.md`
- Modify: `README.md`

**Step 1: Write the contract doc**

Document:
- transition states
- controller ownership values
- action classes
- enforcement rules
- shadow and compare report contracts

**Step 2: Update repo status text**

Record that Phase 13 adds explicit project transition state and authority gating.

**Step 3: Verify docs are readable**

Run: `sed -n '1,220p' docs/contracts/project-transition.md && sed -n '1,120p' README.md`

Expected: both files describe Phase 13 without contradicting earlier project-governance rules.

### Task 2: Add failing store tests for transition state and report persistence

**Files:**
- Modify: `internal/store/sqlite/store_test.go`

**Step 1: Write failing tests**

Add tests for:
- initializing a project's current transition state
- updating transition state and controller
- recording shadow observations
- recording compare reports
- appending the expected transition events

**Step 2: Run the focused test**

Run: `go test ./internal/store/sqlite -run 'TestProjectTransition|TestTransitionReport'`

Expected: FAIL because the schema, models, and store methods do not exist yet.

### Task 3: Implement the runtime schema, models, and store methods

**Files:**
- Create: `internal/store/sqlite/migrations/0005_project_transitions.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/runtime/events/events.go`

**Step 1: Add the schema**

Add:
- `project_transitions`
- `project_transition_reports`

Make transition state current-per-project and reports append-only.

**Step 2: Add models and params**

Add typed models and params for:
- transition state
- state updates
- shadow observations
- compare reports

**Step 3: Add event types and payloads**

Add typed events for:
- `project.transition_changed`
- `project.shadow_observation_recorded`
- `project.compare_report_recorded`
- `project.transition_denied`

**Step 4: Implement minimal store methods**

Implement only what the failing tests need:
- create or initialize transition state
- update transition state
- get current transition state by project id
- append transition reports

**Step 5: Run the focused store test**

Run: `go test ./internal/store/sqlite -run 'TestProjectTransition|TestTransitionReport'`

Expected: PASS

### Task 4: Add failing gate tests for transition enforcement

**Files:**
- Create: `internal/core/projects/transition_test.go`

**Step 1: Write failing tests**

Cover:
- `inventory`, `shadow`, and `compare` rejecting mutation
- `limited_action` allowing only explicit low-risk actions
- `cutover` allowing normal mutation
- controller mismatch denying mutation
- transition control requiring explicit state change

**Step 2: Run the focused gate test**

Run: `go test ./internal/core/projects -run 'TestTransition'`

Expected: FAIL because the transition types and gate do not exist yet.

### Task 5: Implement the transition types and authorization gate

**Files:**
- Create: `internal/core/projects/transition.go`
- Modify: `internal/core/projects/register.go`

**Step 1: Add typed transition definitions**

Add:
- transition states
- controller values
- action classes
- low-risk allowlist shape
- decision result with explicit denial reason

**Step 2: Implement the gate**

Add a reusable function that answers whether an action is allowed for a project given:
- transition state
- controller
- action class
- optional limited-action allowlist

**Step 3: Expose helpers that callers can reuse**

Keep the API small and deterministic.

**Step 4: Run the focused gate test**

Run: `go test ./internal/core/projects -run 'TestTransition'`

Expected: PASS

### Task 6: Add failing projection tests for portfolio transition visibility

**Files:**
- Create: `internal/runtime/projections/transition_test.go`

**Step 1: Write failing tests**

Add tests that prove projections can list:
- project transition state
- current controller
- recent shadow or compare activity

**Step 2: Run the focused projection test**

Run: `go test ./internal/runtime/projections -run 'TestProjectTransitionProjection'`

Expected: FAIL because the projection query does not include transition data yet.

### Task 7: Implement transition-aware projections

**Files:**
- Modify: `internal/runtime/projections/projections.go`

**Step 1: Extend the transition view**

Include:
- transition state
- controller
- last report type
- last report time

**Step 2: Keep projections read-only**

Use SQL queries only. Do not introduce mutable caches.

**Step 3: Run the focused projection test**

Run: `go test ./internal/runtime/projections -run 'TestProjectTransitionProjection'`

Expected: PASS

### Task 8: Wire compare and shadow reporting through a small runtime service

**Files:**
- Create: `internal/core/projects/service.go`
- Create: `internal/core/projects/service_test.go`

**Step 1: Write failing service tests**

Cover:
- recording a shadow observation in shadow mode
- recording a compare report in compare mode
- rejecting those writes when the project state is incompatible
- returning explicit errors for denied transition mutations

**Step 2: Run the focused service test**

Run: `go test ./internal/core/projects -run 'TestTransitionService'`

Expected: FAIL because the service does not exist yet.

**Step 3: Implement the minimal service**

Use the new store methods and gate:
- `SetTransitionState`
- `RecordShadowObservation`
- `RecordCompareReport`
- `AuthorizeAction`

**Step 4: Run the focused service test**

Run: `go test ./internal/core/projects -run 'TestTransitionService'`

Expected: PASS

### Task 9: Run repo-wide verification

**Files:**
- Verify only

**Step 1: Run formatting check**

Run: `make fmtcheck`

Expected: exit 0

**Step 2: Run lint**

Run: `make lint`

Expected: exit 0

**Step 3: Run tests**

Run: `make test`

Expected: exit 0

**Step 4: Run build**

Run: `make build`

Expected: exit 0

### Task 10: Commit the implementation

**Files:**
- Commit all Phase 13 implementation files

**Step 1: Review the final diff**

Run: `git status --short && git diff --stat`

Expected: only Phase 13 files and generated changes are present.

**Step 2: Commit**

Run:

```bash
git add README.md docs/contracts/project-transition.md docs/plans/2026-04-09-phase-13-project-transition.md internal/core/projects internal/runtime/events/events.go internal/runtime/projections/projections.go internal/store/sqlite internal/store/sqlite/migrations/0005_project_transitions.sql
git commit -m "feat: add project transition ladder for phase 13"
```

Expected: one implementation commit for Phase 13.
