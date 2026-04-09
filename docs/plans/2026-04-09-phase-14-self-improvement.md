# Phase 14 Self-Improvement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a proposal-driven self-improvement pipeline that records proposals, evaluates them reproducibly, promotes only approved runtime changes, and supports explicit rollback without editing canonical authored files.

**Architecture:** Store proposals, evaluations, and promotions in SQLite; extend the runtime event model for learning lifecycle events; add typed learning services for proposals, evaluator, replay fixtures, and promotion; and expose read-only views for proposal and promotion visibility. Promotion changes runtime-active records only, and rollback restores the previously active promotion when one exists.

**Tech Stack:** Go, SQLite, embedded SQL migrations, existing runtime events and projections, Go unit tests

---

### Task 1: Add the self-improvement contract docs

**Files:**
- Create: `docs/contracts/self-improvement.md`
- Modify: `docs/contracts/runtime-events.md`
- Modify: `README.md`

**Step 1: Write the contract doc**

Document:
- proposal types
- proposal lifecycle statuses
- evaluation modes
- promotion and rollback rules
- the rule that canonical files are not edited during Phase 14 promotion

**Step 2: Extend the runtime event contract**

Add the Phase 14 learning events and stream types.

**Step 3: Update repo status text**

Record that Phase 14 adds evaluated self-improvement with promotion gating and rollback.

**Step 4: Verify docs are readable**

Run: `sed -n '1,220p' docs/contracts/self-improvement.md && sed -n '1,220p' docs/contracts/runtime-events.md && sed -n '1,120p' README.md`

Expected: all three files describe Phase 14 consistently.

### Task 2: Add failing store tests for the full learning lifecycle

**Files:**
- Modify: `internal/store/sqlite/store_test.go`

**Step 1: Write failing tests**

Add tests for:
- creating and submitting a proposal
- recording a deterministic evaluation
- promoting a proposal into active runtime state
- promoting a newer proposal that supersedes an active promotion
- rolling back the current promotion and restoring the previous one
- appending the expected learning events

**Step 2: Run the focused store test**

Run: `go test ./internal/store/sqlite -run 'TestLearning'`

Expected: FAIL because the schema, models, and store methods do not exist yet.

### Task 3: Implement the learning schema, models, store methods, and event types

**Files:**
- Create: `internal/store/sqlite/migrations/0006_learning.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/runtime/events/events.go`

**Step 1: Add the schema**

Add:
- `learning_proposals`
- `learning_evaluations`
- `learning_promotions`

Keep promotions status-based so rollback can restore a superseded promotion.

**Step 2: Add typed models and params**

Add typed records for:
- proposals
- evaluations
- promotions

**Step 3: Add learning event types and payloads**

Add:
- `learning.proposal_created`
- `learning.proposal_submitted`
- `learning.evaluation_recorded`
- `learning.proposal_rejected`
- `learning.promotion_applied`
- `learning.promotion_rolled_back`

**Step 4: Implement minimal store methods**

Implement only what the failing tests need:
- create proposal
- update proposal status
- get proposal
- record evaluation
- promote proposal
- rollback promotion
- list active promotions and evaluations

**Step 5: Run the focused store test**

Run: `go test ./internal/store/sqlite -run 'TestLearning'`

Expected: PASS

### Task 4: Add failing evaluator tests for deterministic replay and sandbox scoring

**Files:**
- Create: `internal/learning/evaluator/service_test.go`
- Create: `internal/learning/replay/types_test.go`

**Step 1: Write failing tests**

Cover:
- deterministic scoring from replay inputs
- deterministic scoring from sandbox inputs
- stable weighting of success, cost, latency, and violations
- reproducible results from the same fixture

**Step 2: Run the focused evaluator tests**

Run: `go test ./internal/learning/evaluator ./internal/learning/replay`

Expected: FAIL because the evaluator and replay fixture types do not exist yet.

### Task 5: Implement replay fixtures and evaluator scoring

**Files:**
- Create: `internal/learning/replay/types.go`
- Create: `internal/learning/evaluator/service.go`

**Step 1: Add replay fixture types**

Add:
- evaluation mode enum
- fixture key
- baseline metrics
- candidate metrics
- metric weights

**Step 2: Implement deterministic scoring**

Compute a stable weighted score and verdict from the recorded fixture only.

**Step 3: Run the focused evaluator tests**

Run: `go test ./internal/learning/evaluator ./internal/learning/replay`

Expected: PASS

### Task 6: Add failing proposal and promotion service tests

**Files:**
- Create: `internal/learning/proposals/service_test.go`
- Create: `internal/learning/promotion/service_test.go`

**Step 1: Write failing proposal service tests**

Cover:
- creating a draft proposal
- submitting a proposal
- rejecting a proposal

**Step 2: Write failing promotion service tests**

Cover:
- evaluating and approving a proposal
- promoting an approved proposal
- superseding an active promotion with a newer proposal
- rolling back and restoring the previous promotion
- confirming no file edits happen as part of promotion

**Step 3: Run the focused service tests**

Run: `go test ./internal/learning/proposals ./internal/learning/promotion`

Expected: FAIL because the services do not exist yet.

### Task 7: Implement proposal and promotion services

**Files:**
- Create: `internal/learning/proposals/service.go`
- Create: `internal/learning/promotion/service.go`

**Step 1: Implement proposal lifecycle methods**

Add:
- `Create`
- `Submit`
- `Reject`

**Step 2: Implement promotion lifecycle methods**

Add:
- `Promote`
- `Rollback`
- `ListActive`

Require:
- approved evaluation before promotion
- explicit runtime-only activation
- restoration of superseded promotion on rollback when available

**Step 3: Run the focused service tests**

Run: `go test ./internal/learning/proposals ./internal/learning/promotion`

Expected: PASS

### Task 8: Add a read-only visibility surface for operators

**Files:**
- Modify: `internal/runtime/projections/projections.go`
- Create: `internal/runtime/projections/learning_test.go`

**Step 1: Write failing projection tests**

Cover:
- proposal status visibility
- latest evaluation visibility
- active promotion visibility

**Step 2: Run the focused projection test**

Run: `go test ./internal/runtime/projections -run 'TestLearning'`

Expected: FAIL because the read-only learning views do not exist yet.

**Step 3: Implement the projection views**

Add read-only query helpers only.

**Step 4: Run the focused projection test**

Run: `go test ./internal/runtime/projections -run 'TestLearning'`

Expected: PASS

### Task 9: Run repo-wide verification

**Files:**
- Verify only

**Step 1: Run formatting check**

Run: `make format && make fmtcheck`

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
- Commit all Phase 14 implementation files

**Step 1: Review the final diff**

Run: `git status --short && git diff --stat`

Expected: only Phase 14 files and generated changes are present.

**Step 2: Commit**

Run:

```bash
git add README.md docs/contracts/self-improvement.md docs/contracts/runtime-events.md docs/plans/2026-04-09-phase-14-self-improvement.md internal/learning internal/runtime/events/events.go internal/runtime/projections internal/store/sqlite internal/store/sqlite/migrations/0006_learning.sql
git commit -m "feat: add evaluated self-improvement pipeline for phase 14"
```

Expected: one implementation commit for Phase 14.
