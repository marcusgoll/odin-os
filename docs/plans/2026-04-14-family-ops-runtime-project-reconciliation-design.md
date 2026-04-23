# Family-Ops Runtime Project Reconciliation Design

## Context

`family-ops` is now configured for `cutover` through the machine-local Odin overlay, but live task execution still leases worktrees from the old shadow checkout. The root cause is that Odin persists project metadata in the runtime database and `ensureRuntimeProject()` currently treats that row as immutable after first creation.

That means a manifest change to `git_root`, `default_branch`, or basic project metadata does not affect future runs until the runtime database is manually edited. For a cutover system, that is the wrong failure mode.

## Problem

The current behavior makes project source selection drift-prone:

- overlay and registry point `family-ops` at the cutover checkout and branch
- runtime `projects` row still points at the old shadow checkout and `main`
- new tasks reuse stale runtime project metadata
- leased worktrees are created from the wrong source line

This breaks the first real goal-driven `family-ops` Odin task even when the operator configuration is otherwise correct.

## Approaches

### Reconcile runtime project rows against the current manifest (recommended)

Whenever Odin resolves a manifest into a runtime project, update the stored row if any persisted metadata has changed.

Pros:

- smallest fix
- keeps runtime state honest after overlay or manifest changes
- no operator-side repair step
- applies to all managed projects, not only `family-ops`

Cons:

- requires carefully scoped update logic so we do not rewrite unrelated state

### Add an operator-only repair command

Require the operator to run a CLI command whenever project metadata changes.

Pros:

- simple implementation

Cons:

- wrong operational model
- easy to forget
- leaves normal task execution vulnerable to stale source selection

### Delete and recreate runtime project rows on drift

Pros:

- brute-force correct

Cons:

- risks breaking historical joins and transition state
- far more invasive than needed

## Recommendation

Use manifest reconciliation during `ensureRuntimeProject()`.

The runtime `projects` row should be a cached projection of the current manifest, not a one-time copy. If the current manifest says the repo root or default branch changed, Odin should update the stored row before the scheduler creates the next lease.

## Design

### Reconciliation scope

When Odin resolves a manifest for an existing project row, compare and update:

- `name`
- `scope`
- `git_root`
- `default_branch`
- `github_repo`
- `manifest_path`

This stays intentionally narrow. It updates only persisted manifest metadata and leaves task history, transitions, approvals, and leases untouched.

### Scheduler behavior

`runQueuedTask()` already reads `project.GitRoot` and `project.DefaultBranch` from the runtime database before preparing a lease. Once reconciliation happens in `ensureRuntimeProject()`, future runs will automatically lease from the corrected repo root and base branch.

### Operator expectations

After the fix:

- changing the overlay or manifest is enough
- the next `ensureRuntimeProject()` call refreshes the runtime row
- new tasks lease from the latest configured repo root and default branch
- no manual SQLite edits are required

Existing released leases remain historical records. Active leases continue to reflect the row values that were used when they were created.

## Testing

Add a regression test proving:

- a project row created from one manifest snapshot is later reconciled when the manifest changes
- the updated row contains the new `git_root` and `default_branch`
- scheduler-facing code reads the reconciled values

## Success Criteria

- live runtime project rows converge to current overlay/manifest metadata
- a new `family-ops` intake-backed task leases from the cutover checkout
- the leased task branch is based on `odin-cutover-main`, not the old shadow line
