# Phase 07 Tool Broker Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a dynamic tool broker that exposes thin capability cards first, expands full definitions on demand, enforces tool and context budgets, and returns compactable structured results.

**Architecture:** Add a thin capability catalog under `internal/tools/catalog`, explicit budget rules under `internal/tools/budgets`, and a broker service under `internal/tools/broker` that composes built-in tool definitions with registry-backed skills and agents. Integrate the broker with the planner path so planning starts from thin cards and expands only chosen capabilities.

**Tech Stack:** Go, existing registry snapshot types, table-driven tests, structured result envelopes

---

### Task 1: Define catalog and budget contracts

**Files:**
- Create: `docs/contracts/capability-catalog.md`
- Create: `docs/contracts/capability-budgets.md`
- Create: `internal/tools/catalog/types.go`
- Create: `internal/tools/catalog/types_test.go`
- Create: `internal/tools/budgets/budgets.go`
- Create: `internal/tools/budgets/budgets_test.go`

**Step 1: Write the failing test**

Add tests that expect:
- thin capability cards for `tool`, `skill`, and `sub_agent`
- cards omit full definitions
- tool and context budget enforcement rejects overuse explicitly

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/catalog ./internal/tools/budgets`
Expected: FAIL because catalog and budget types do not exist yet.

**Step 3: Write minimal implementation**

Implement catalog card types, expansion result types, and budget enforcement helpers with explicit denial reasons.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/catalog ./internal/tools/budgets`
Expected: PASS

**Step 5: Commit**

```bash
git add docs/contracts/capability-catalog.md docs/contracts/capability-budgets.md internal/tools/catalog/types.go internal/tools/catalog/types_test.go internal/tools/budgets/budgets.go internal/tools/budgets/budgets_test.go
git commit -m "feat: add capability catalog and budget contracts"
```

### Task 2: Build the broker and built-in tool inventory

**Files:**
- Create: `internal/tools/catalog/builtin.go`
- Create: `internal/tools/catalog/builtin_test.go`
- Create: `internal/tools/broker/broker.go`
- Create: `internal/tools/broker/broker_test.go`

**Step 1: Write the failing test**

Add tests that expect:
- thin catalog cards from built-in tools and registry-backed skills/sub-agents
- expansion returns full selected definitions only
- structured invocation results compact cleanly
- tool invocation respects budget rules

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/catalog ./internal/tools/broker`
Expected: FAIL because the broker and built-in tool catalog do not exist yet.

**Step 3: Write minimal implementation**

Implement built-in tool definitions, thin catalog generation, expansion, invocation, and compaction logic.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/catalog ./internal/tools/broker`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/tools/catalog/builtin.go internal/tools/catalog/builtin_test.go internal/tools/broker/broker.go internal/tools/broker/broker_test.go
git commit -m "feat: add dynamic tool broker"
```

### Task 3: Integrate the broker with planner-side task preparation

**Files:**
- Create: `internal/workers/planner/service.go`
- Create: `internal/workers/planner/service_test.go`

**Step 1: Write the failing test**

Add tests that expect:
- planner preparation starts from a thin catalog only
- capability expansion happens only for selected keys
- sub-agent expansion is denied when the plan did not request it

**Step 2: Run test to verify it fails**

Run: `go test ./internal/workers/planner`
Expected: FAIL because planner-side broker integration does not exist yet.

**Step 3: Write minimal implementation**

Implement a small planner service that requests thin cards, expands selected capabilities, and returns a compact execution context.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/workers/planner`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/workers/planner/service.go internal/workers/planner/service_test.go
git commit -m "feat: integrate tool broker with planner"
```

### Task 4: Update docs and complete verification

**Files:**
- Modify: `README.md`

**Step 1: Update docs**

Update `README.md` to reflect the broker and token-efficient capability loading.

**Step 2: Run focused verification**

Run: `go test ./internal/tools/... ./internal/workers/planner`
Expected: PASS

**Step 3: Run repo verification**

Run: `make fmtcheck && make lint && make test && make build`
Expected: all commands exit `0`

**Step 4: Review the diff**

Run: `git status --short`
Expected: only intended Prompt 07 files are changed.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add dynamic tool broker for phase 07"
```
