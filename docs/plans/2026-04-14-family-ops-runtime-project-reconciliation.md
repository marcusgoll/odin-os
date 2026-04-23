# Family-Ops Runtime Project Reconciliation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Odin reconcile persisted runtime project metadata with the current manifest so `family-ops` tasks lease from the configured cutover checkout and branch.

**Architecture:** Update `ensureRuntimeProject()` so existing runtime project rows are compared against the current manifest and refreshed when drift is detected. Cover the drift case with a regression test, rebuild the live binary, then verify a new intake-backed `family-ops` task uses the corrected repo root and base branch.

**Tech Stack:** Go, SQLite, Odin runtime jobs service, live `odin-os` systemd deployment

---

### Task 1: Add a failing reconciliation test

**Files:**
- Modify: `internal/runtime/jobs/service_test.go`

**Step 1: Write the failing test**

Add a test that:

- creates a runtime project from an initial manifest with the shadow repo root and `main`
- calls `EnsureRuntimeProject()` again with the same key but a new manifest using the cutover repo root and `odin-cutover-main`
- expects the returned runtime project to contain the updated values

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/jobs -run TestEnsureRuntimeProjectReconcilesManifestDrift -count=1`

Expected: FAIL because the existing row is returned unchanged.

**Step 3: Commit**

Do not commit yet. Continue once the test is red.

### Task 2: Implement manifest reconciliation

**Files:**
- Modify: `internal/runtime/jobs/service.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/models.go` if a new update params struct is needed

**Step 1: Write minimal implementation**

Add a store update path for project metadata and change `ensureRuntimeProject()` so it:

- loads the existing row
- compares persisted manifest-backed fields
- updates the row when drift is detected
- returns the reconciled row

Keep the update narrow to manifest metadata only.

**Step 2: Run targeted test**

Run: `go test ./internal/runtime/jobs -run TestEnsureRuntimeProjectReconcilesManifestDrift -count=1`

Expected: PASS

**Step 3: Run nearby runtime/store tests**

Run: `go test ./internal/runtime/jobs ./internal/store/sqlite -count=1`

Expected: PASS

### Task 3: Verify live runtime behavior

**Files:**
- No repo file changes required

**Step 1: Rebuild and redeploy live Odin**

Run the local build/restart sequence used for the live sidecar so the patched binary is active.

**Step 2: Verify runtime project row**

Run:

```bash
sqlite3 /home/orchestrator/.local/state/odin-os/data/odin.db "select key,git_root,default_branch from projects where key='family-ops';"
```

Expected: `family-ops` now points at the cutover checkout and `odin-cutover-main`.

**Step 3: Verify with a new intake-backed task**

Enqueue a fresh `family-ops` intake task and confirm:

- the new lease `repo_root` is the cutover checkout
- the leased worktree contains the Plaid-capable line
- the task prompt still includes the goal-driven intake sections

**Step 4: Commit**

Do not commit yet. Continue into the `family-ops` implementation.
