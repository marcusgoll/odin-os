# Phase 05 Interactive CLI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the Odin interactive shell with local Ask mode, scoped Act mode, slash commands, header rendering, and light session persistence.

**Architecture:** Add a testable shell service in `internal/cli/repl`, route slash commands through `internal/cli/commands`, render shell output from `internal/cli/render`, and expose runtime-backed listings and task creation through narrow services in `internal/runtime/*`. Persist only preferred mode and last project key in `state/cache/cli-session.json`, with safe downgrade on startup.

**Tech Stack:** Go, SQLite via existing store, YAML-backed project manifests, table-driven tests, `bufio` REPL loop, JSON session cache

---

### Task 1: Add session state, mode rules, and header rendering contracts

**Files:**
- Create: `docs/contracts/cli-session.md`
- Create: `internal/cli/repl/session.go`
- Create: `internal/cli/repl/session_test.go`
- Create: `internal/cli/render/header.go`
- Create: `internal/cli/render/header_test.go`

**Step 1: Write the failing test**

Add tests that expect:
- session cache load and save
- invalid saved project downgrades to global
- invalid saved mode downgrades to ask
- header rendering always includes scope, mode, health, approvals, and active task or run when present

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/repl ./internal/cli/render`
Expected: FAIL because session and header implementations do not exist yet.

**Step 3: Write minimal implementation**

Implement session state structs, JSON persistence, safe restore logic, and header rendering helpers.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/repl ./internal/cli/render`
Expected: PASS for session and header behavior.

**Step 5: Commit**

```bash
git add docs/contracts/cli-session.md internal/cli/repl/session.go internal/cli/repl/session_test.go internal/cli/render/header.go internal/cli/render/header_test.go
git commit -m "feat: add cli session and header state"
```

### Task 2: Add runtime-backed shell services

**Files:**
- Create: `internal/runtime/health/service.go`
- Create: `internal/runtime/health/service_test.go`
- Create: `internal/runtime/jobs/service.go`
- Create: `internal/runtime/jobs/service_test.go`
- Create: `internal/runtime/runs/service.go`
- Create: `internal/runtime/runs/service_test.go`
- Modify: `internal/store/sqlite/store.go`

**Step 1: Write the failing test**

Add tests that expect:
- health summary from database and manifest state
- job listing filtered by scope
- Act-mode task creation to ensure runtime project rows and create queued tasks
- run listing filtered by scope

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/health ./internal/runtime/jobs ./internal/runtime/runs`
Expected: FAIL because these services and required store helpers do not exist yet.

**Step 3: Write minimal implementation**

Implement focused services for health, task listing/creation, and run listing. Add only the store helpers needed to look up runtime projects safely.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/health ./internal/runtime/jobs ./internal/runtime/runs`
Expected: PASS for service behavior.

**Step 5: Commit**

```bash
git add internal/runtime/health/service.go internal/runtime/health/service_test.go internal/runtime/jobs/service.go internal/runtime/jobs/service_test.go internal/runtime/runs/service.go internal/runtime/runs/service_test.go internal/store/sqlite/store.go
git commit -m "feat: add runtime services for interactive cli"
```

### Task 3: Add slash-command parsing and local Ask routing

**Files:**
- Create: `internal/cli/commands/commands.go`
- Create: `internal/cli/commands/commands_test.go`

**Step 1: Write the failing test**

Add tests that expect:
- slash command parsing for required commands
- `/mode`, `/scope`, `/project`, and `/self` updates
- Ask-mode free text routing to local operational answers
- unknown Ask-mode free text returns a bounded fallback without creating work

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/commands`
Expected: FAIL because command parsing and Ask routing do not exist yet.

**Step 3: Write minimal implementation**

Implement slash-command parsing, a small command handler interface, and a local Ask intent router based on operational keywords.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/commands`
Expected: PASS for command and Ask behavior.

**Step 5: Commit**

```bash
git add internal/cli/commands/commands.go internal/cli/commands/commands_test.go
git commit -m "feat: add cli command parsing and local ask routing"
```

### Task 4: Build the interactive shell loop and lifecycle integration

**Files:**
- Create: `internal/cli/repl/shell.go`
- Create: `internal/cli/repl/shell_test.go`
- Create: `internal/app/bootstrap/bootstrap.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `cmd/odin/main.go`

**Step 1: Write the failing test**

Add tests that expect:
- `odin` shell startup restores saved mode and project when valid
- startup downgrades safely when saved state is invalid
- Ask mode handles free text without creating tasks
- Act mode creates structured runtime tasks in project or `odin-core` scope
- Act mode is rejected in global scope

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/repl ./internal/app/lifecycle`
Expected: FAIL because the shell loop and lifecycle integration do not exist yet.

**Step 3: Write minimal implementation**

Implement the REPL loop, bootstrap runtime dependencies, wire `odin` to start the shell, and expose a testable `HandleLine` path.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/repl ./internal/app/lifecycle`
Expected: PASS for shell behavior and startup wiring.

**Step 5: Commit**

```bash
git add internal/cli/repl/shell.go internal/cli/repl/shell_test.go internal/app/bootstrap/bootstrap.go internal/app/lifecycle/run.go internal/app/lifecycle/run_test.go cmd/odin/main.go
git commit -m "feat: add interactive odin shell"
```

### Task 5: Update docs and complete repo verification

**Files:**
- Modify: `README.md`

**Step 1: Update docs**

Update `README.md` to reflect the new primary interface.

**Step 2: Run focused package tests**

Run: `go test ./internal/cli/... ./internal/runtime/... ./internal/app/lifecycle`
Expected: PASS

**Step 3: Run repo verification**

Run: `make fmtcheck && make lint && make test && make build`
Expected: all commands exit `0`

**Step 4: Review the diff**

Run: `git status --short`
Expected: only intended Prompt 05 files are changed.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add interactive cli shell for phase 05"
```
