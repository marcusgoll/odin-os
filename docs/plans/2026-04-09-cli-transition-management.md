# CLI Transition Management Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add operator-facing transition status, transition changes, and shadow/compare report commands to the Odin CLI without manual database edits.

**Architecture:** Keep transition authority in the existing `internal/core/projects` service and add only a thin REPL command layer. The shell should resolve the current project, ensure the runtime project row exists when needed, call the existing transition methods, and render clear operator output with explicit reason and confirmation rules.

**Tech Stack:** Go, existing REPL shell, existing SQLite-backed transition service, Go unit tests

---

### Task 1: Add failing shell tests for transition commands

**Files:**
- Modify: `internal/cli/repl/shell_test.go`

**Step 1: Write the failing tests**

Add tests for:
- `/help` includes `/transition`, `/observe`, and `/compare`
- `/transition` in project scope shows inventory and legacy authority
- `/transition set shadow because ...` succeeds and records an event
- `/transition set cutover because ...` fails without `confirm`
- `/transition set limited_action confirm because ...` fails without `allow=...`
- `/observe ...` in `shadow` records a report
- `/compare ...` in `compare` records a report
- `/transition` in global scope is rejected

**Step 2: Run the focused tests**

Run: `go test ./internal/cli/repl -run 'TestShell(Help|Transition|Observe|Compare)' -count=1`
Expected: FAIL because the commands do not exist yet.

### Task 2: Implement shell command parsing and handlers

**Files:**
- Modify: `internal/cli/repl/shell.go`

**Step 1: Add command handling**

Implement handlers for:
- `/transition`
- `/transition status`
- `/transition set ...`
- `/observe`
- `/compare`

**Step 2: Keep the implementation minimal**

Use:
- current scope validation
- runtime project lookup/create
- existing transition service methods
- small shell-side parsing for `because`, `confirm`, and `allow=...`

**Step 3: Re-run the focused tests**

Run: `go test ./internal/cli/repl -run 'TestShell(Help|Transition|Observe|Compare)' -count=1`
Expected: PASS

### Task 3: Update help text and command docs

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Create: `docs/operations/project-transitions.md`

**Step 1: Update `/help` output**

Include:
- `/transition`
- `/observe`
- `/compare`

**Step 2: Add operator docs**

Document:
- command syntax
- reason/confirmation rules
- recommended shadow-mode usage

### Task 4: Run verification

**Files:**
- No new files

**Step 1: Focused verification**

Run:
- `go test ./internal/cli/repl -count=1`
- `go test ./internal/core/projects -count=1`

Expected: PASS

**Step 2: Sanity verification**

Run:
- `go test ./internal/cli/commands -count=1`

Expected: PASS
