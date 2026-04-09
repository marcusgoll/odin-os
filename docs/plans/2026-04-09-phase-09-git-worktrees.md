# Phase 09 Git Worktrees Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add task-owned branch naming, global-root worktree paths, and SQLite-backed worktree leases so mutating Odin tasks run in isolated worktrees and read-only tasks bypass mutable allocation safely.

**Architecture:** Add a small VCS stack split across `internal/vcs/branches`, `internal/vcs/worktrees`, `internal/vcs/git`, and `internal/vcs/leases`, backed by a new `worktree_leases` table in SQLite. Keep lease authority in the runtime store, keep path and branch rules deterministic, and make the manager return a read-only execution context when mutation is not required.

**Tech Stack:** Go, SQLite via `database/sql`, embedded SQL migrations, Git CLI, Go tests

---

### Task 1: Lock branch naming and path rules with failing tests

**Files:**
- Create: `docs/contracts/git-worktrees.md`
- Create: `internal/vcs/branches/naming_test.go`
- Create: `internal/vcs/worktrees/paths_test.go`

**Step 1: Write the failing branch naming tests**

Cover:
- task-owned branch names with project, task, run, and retry identity
- retry suffix increments
- no agent identity in branch names

**Step 2: Write the failing worktree path tests**

Cover:
- immediate default root `~/.config/superpowers/worktrees/odin-os/`
- documented long-term defaults
- deterministic path generation outside repo roots

**Step 3: Run the focused VCS test command and verify it fails**

Run: `go test ./internal/vcs/branches ./internal/vcs/worktrees`
Expected: FAIL because the packages and naming/path logic do not exist yet.

**Step 4: Add the contract doc and minimal naming/path implementation**

Implement only the minimum code needed to satisfy the branch and path tests.

**Step 5: Re-run the focused VCS test command**

Run: `go test ./internal/vcs/branches ./internal/vcs/worktrees`
Expected: PASS

### Task 2: Add SQLite lease storage with failing tests

**Files:**
- Create: `internal/store/sqlite/migrations/0003_worktree_leases.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Create: `internal/store/sqlite/worktree_leases_test.go`

**Step 1: Write the failing lease storage tests**

Cover:
- create mutable lease
- conflict on concurrent mutable lease for same task or path
- heartbeat update
- release transition
- cleanup query for released or stale leases

**Step 2: Run the focused store test command and verify it fails**

Run: `go test ./internal/store/sqlite -run 'TestWorktreeLease|TestCleanupEligible'`
Expected: FAIL because the new migration and lease methods do not exist yet.

**Step 3: Implement the migration and store methods**

Add:
- `worktree_leases` table
- lease models
- create, heartbeat, release, list, and cleanup-selection methods

Keep lease decisions deterministic and error messages explicit.

**Step 4: Re-run the focused store test command**

Run: `go test ./internal/store/sqlite -run 'TestWorktreeLease|TestCleanupEligible'`
Expected: PASS

### Task 3: Add the git adapter and lease manager with failing tests

**Files:**
- Create: `internal/vcs/git/adapter.go`
- Create: `internal/vcs/git/adapter_test.go`
- Create: `internal/vcs/leases/manager.go`
- Create: `internal/vcs/leases/manager_test.go`

**Step 1: Write the failing manager tests**

Cover:
- mutating task gets one branch and one worktree
- read-only task skips mutable allocation
- attach to same active lease for same task and run
- conflicting mutable claims are denied

**Step 2: Write the failing git adapter tests**

Use temp repos to cover:
- branch existence checks
- worktree create and remove flow
- non-destructive inspection helpers

**Step 3: Run the focused VCS manager tests and verify they fail**

Run: `go test ./internal/vcs/git ./internal/vcs/leases`
Expected: FAIL because the manager and git adapter do not exist yet.

**Step 4: Implement the minimal git adapter and lease manager**

Keep shelling centralized in `internal/vcs/git`, and keep policy-free orchestration in the lease manager.

**Step 5: Re-run the focused VCS manager tests**

Run: `go test ./internal/vcs/git ./internal/vcs/leases`
Expected: PASS

### Task 4: Add worktree cleanup behavior and full-phase verification

**Files:**
- Create: `internal/vcs/worktrees/manager.go`
- Create: `internal/vcs/worktrees/manager_test.go`
- Modify: `README.md`
- Modify: `docs/contracts/runtime-events.md` if lease events are introduced

**Step 1: Write the failing cleanup tests**

Cover:
- released leases clean up deterministically
- active leases are preserved
- cleanup can return inspectable results

**Step 2: Run the focused cleanup tests and verify they fail**

Run: `go test ./internal/vcs/worktrees`
Expected: FAIL because cleanup orchestration is incomplete.

**Step 3: Implement cleanup and any required orchestration glue**

Add only the code needed to satisfy cleanup behavior and inspection.

**Step 4: Run focused verification**

Run: `go test ./internal/vcs/... ./internal/store/sqlite`
Expected: PASS

**Step 5: Run full verification**

Run: `make fmtcheck && make lint && make test && make build`
Expected: PASS

**Step 6: Commit**

```bash
git add README.md docs/contracts/git-worktrees.md docs/plans/2026-04-09-phase-09-git-worktrees.md internal/store/sqlite/migrations/0003_worktree_leases.sql internal/store/sqlite/models.go internal/store/sqlite/store.go internal/store/sqlite/worktree_leases_test.go internal/vcs/branches internal/vcs/git internal/vcs/leases internal/vcs/worktrees
git commit -m "feat: add git worktrees and leases for phase 09"
```
