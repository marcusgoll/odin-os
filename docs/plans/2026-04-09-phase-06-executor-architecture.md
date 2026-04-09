# Phase 06 Executor Architecture Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a model-agnostic executor contract, declarative routing config, skeletal adapters, and fallback-aware route selection for portable Odin tasks.

**Architecture:** Define one strongly typed portable task spec and executor interface in `internal/executors/contract`, parse executor inventory and route rules from authored YAML, and implement a small router that selects healthy capability-compatible executors by primary and fallback order. Provider packages expose skeletal adapters only in this phase.

**Tech Stack:** Go, YAML config parsing with `gopkg.in/yaml.v3`, table-driven tests, compile-ready adapter skeletons

---

### Task 1: Define the executor contract

**Files:**
- Create: `docs/contracts/executor-contract.md`
- Create: `internal/executors/contract/types.go`
- Create: `internal/executors/contract/types_test.go`

**Step 1: Write the failing test**

Add tests that expect:
- executor class constants
- portable task spec requirements
- capability matching behavior for required classes and features

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executors/contract`
Expected: FAIL because the contract types and helpers do not exist yet.

**Step 3: Write minimal implementation**

Implement the typed contract, capability requirements, matching helpers, and common not-implemented error values.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/executors/contract`
Expected: PASS

**Step 5: Commit**

```bash
git add docs/contracts/executor-contract.md internal/executors/contract/types.go internal/executors/contract/types_test.go
git commit -m "feat: add executor contract"
```

### Task 2: Add declarative routing config and selector

**Files:**
- Create: `docs/contracts/executor-routing.md`
- Modify: `config/executors.yaml`
- Modify: `config/models.yaml`
- Create: `internal/executors/router/config.go`
- Create: `internal/executors/router/router.go`
- Create: `internal/executors/router/router_test.go`

**Step 1: Write the failing test**

Add tests that expect:
- config loading from YAML
- route matching by task kind and scope
- primary executor preference
- fallback selection when the primary is unavailable
- rejection when no executor meets capability requirements

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executors/router`
Expected: FAIL because config loading and route selection do not exist yet.

**Step 3: Write minimal implementation**

Implement typed config parsing and a route selector that filters by enabled executors, route match, capability requirements, and health status.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/executors/router`
Expected: PASS

**Step 5: Commit**

```bash
git add docs/contracts/executor-routing.md config/executors.yaml config/models.yaml internal/executors/router/config.go internal/executors/router/router.go internal/executors/router/router_test.go
git commit -m "feat: add executor routing config and selector"
```

### Task 3: Add skeletal provider adapters

**Files:**
- Create: `internal/executors/codex/adapter.go`
- Create: `internal/executors/claude_code/adapter.go`
- Create: `internal/executors/gemini_cli/adapter.go`
- Create: `internal/executors/openai_api/adapter.go`
- Create: `internal/executors/anthropic_api/adapter.go`
- Create: `internal/executors/google_api/adapter.go`
- Create: `internal/executors/xai_api/adapter.go`
- Create: `internal/executors/openrouter_api/adapter.go`
- Create: `internal/executors/router/catalog.go`
- Create: `internal/executors/router/catalog_test.go`

**Step 1: Write the failing test**

Add tests that expect:
- adapters expose stable keys and executor classes
- adapters return explicit capabilities
- router catalog registers all required skeletons

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executors/router`
Expected: FAIL because the adapter skeletons and catalog do not exist yet.

**Step 3: Write minimal implementation**

Implement compile-ready adapters with static capabilities and `ErrNotImplemented` execution methods. Add a router catalog that returns all known adapters.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/executors/router`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/executors/codex/adapter.go internal/executors/claude_code/adapter.go internal/executors/gemini_cli/adapter.go internal/executors/openai_api/adapter.go internal/executors/anthropic_api/adapter.go internal/executors/google_api/adapter.go internal/executors/xai_api/adapter.go internal/executors/openrouter_api/adapter.go internal/executors/router/catalog.go internal/executors/router/catalog_test.go
git commit -m "feat: add executor adapter skeletons"
```

### Task 4: Update docs and complete verification

**Files:**
- Modify: `README.md`

**Step 1: Update docs**

Update `README.md` to reflect the executor abstraction layer.

**Step 2: Run focused verification**

Run: `go test ./internal/executors/...`
Expected: PASS

**Step 3: Run repo verification**

Run: `make fmtcheck && make lint && make test && make build`
Expected: all commands exit `0`

**Step 4: Review the diff**

Run: `git status --short`
Expected: only intended Prompt 06 files are changed.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add model-agnostic executor architecture for phase 06"
```
