# Phase 22 Safe Infrastructure Promotion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Promote the action-key and lease operator infrastructure onto a `main`-based branch while keeping bounded mutation execution disabled.

**Architecture:** Reapply the SQLite/task/event changes, project manifest schema, fail-closed jobs-runtime checks, and REPL lease surfaces from the experimental line, but do not port any executor mutation behavior or experimental `pbs` policy. The branch must remain safe by default.

**Tech Stack:** Go, SQLite, REPL shell, git worktree manager

---

### Task 1: Add failing tests for safe action-key persistence

**Files:**
- Modify: `internal/runtime/jobs/service_test.go`
- Modify: `internal/store/sqlite/store_test.go`
- Modify: `internal/core/projects/manifest_test.go`
- Modify: `internal/core/projects/register_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `CreateTaskFromAct` parsing `action:<key> ...`
- task persistence carrying `action_key`
- manifest parsing of `limited_actions` rule metadata

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/runtime/jobs ./internal/store/sqlite ./internal/core/projects -count=1
```

Expected:

- FAIL because `main` does not yet persist or parse the safe subset

### Task 2: Implement schema, store, and event support

**Files:**
- Create: `internal/store/sqlite/migrations/0007_task_action_keys.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/runtime/events/events.go`

**Step 1: Implement minimal code**

Add `task.action_key` support and ensure `task.created` carries it.

**Step 2: Run tests**

```bash
go test ./internal/store/sqlite -count=1
```

Expected:

- PASS for store coverage

### Task 3: Add manifest and validation support for bounded action declarations

**Files:**
- Create: `internal/core/projects/limited_actions.go`
- Modify: `internal/core/projects/manifest.go`
- Modify: `internal/core/projects/validate.go`
- Modify: `internal/core/projects/manifest_test.go`
- Modify: `internal/core/projects/register_test.go`

**Step 1: Implement minimal code**

Add bounded-action declaration parsing and validation, but do not modify operational `pbs` policy.

**Step 2: Run tests**

```bash
go test ./internal/core/projects -count=1
```

Expected:

- PASS

### Task 4: Add fail-closed runtime handling for explicit bounded actions

**Files:**
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/runtime/jobs/service_test.go`

**Step 1: Write the failing test**

Add a test proving:

- explicit bounded-action tasks are parsed and stored
- execution fails closed before mutation with a clear message on this line

**Step 2: Verify red**

```bash
go test ./internal/runtime/jobs -count=1
```

Expected:

- FAIL until the fail-closed path exists

**Step 3: Implement minimal code**

Keep all bounded-action execution disabled.

**Step 4: Verify green**

```bash
go test ./internal/runtime/jobs -count=1
```

Expected:

- PASS

### Task 5: Promote lease inspection and cleanup shell surfaces

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `internal/vcs/worktrees/manager.go`
- Modify: `internal/vcs/worktrees/manager_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `/leases inspect <lease-id>`
- `/leases cleanup confirm`

**Step 2: Verify red**

```bash
go test ./internal/cli/repl ./internal/vcs/worktrees -count=1
```

Expected:

- FAIL until the shell and manager support exist

**Step 3: Implement minimal code**

Promote only the inert operator surfaces.

**Step 4: Verify green**

```bash
go test ./internal/cli/repl ./internal/vcs/worktrees -count=1
```

Expected:

- PASS

### Task 6: Write safe-promotion docs and audit

**Files:**
- Create: `docs/operations/lease-cleanup-and-inspection.md`
- Create: `docs/operations/limited-action-policy.md`
- Create: `docs/audits/phase-22-safe-infra-promotion.md`
- Modify: `docs/operations/project-transitions.md`

**Step 1: Write docs**

Document:

- what was promoted
- what remains explicitly disabled
- that `pbs` remains shadow-only on `main`

### Task 7: Final verification and commit

**Step 1: Run final verification**

```bash
go test ./internal/runtime/jobs ./internal/core/projects ./internal/cli/repl ./internal/store/sqlite ./internal/vcs/... -count=1
make build
git status --short --branch
```

Expected:

- tests pass
- build succeeds

**Step 2: Commit**

```bash
git add <updated files>
git commit -m "feat: promote safe limited-action infrastructure"
```
