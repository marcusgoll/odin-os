---
title: Odin OS Brownfield Migration Plan
status: draft
date: 2026-04-30
---

# Odin OS Brownfield Migration Plan

## Migration Principles

1. No big-bang rewrite.
2. Keep working Odin runtime behavior intact.
3. Prefer thin adapters over parallel systems.
4. Use `internal/app/lifecycle` as the composition root.
5. Use `internal/store/sqlite` as the runtime authority.
6. Use `internal/executors/contract` as the runner seam.
7. Use `internal/vcs` for all mutating project work.
8. Use top-level `odin ...` commands for operator proof.
9. Treat `docs/plans/*` as design history until command or test evidence proves implementation.
10. Remove accidental scaffolds before adding agency features.

## Recommended Target Architecture

The target is a deepened version of the existing system:

```text
cmd/odin
  -> internal/app/lifecycle
    -> internal/cli commands and REPL aliases
    -> internal/api/http operational surfaces
    -> internal/runtime services
      -> internal/store/sqlite
      -> internal/vcs leases/worktrees/git
      -> internal/executors contract/router/adapters
      -> internal/registry authored assets
      -> internal/core project governance and intake
```

Agency orchestration should be added as small vertical slices over this path:

1. Delivery profile registry entry.
2. `odin work` command behavior.
3. Work Item / Run Attempt projections over existing `tasks` / `runs`.
4. GitHub read-only intake as an intake adapter.
5. Dry-run scheduler.
6. Worktree dispatch.
7. Real `codex_exec` executor.
8. Draft PR handoff.
9. QA/review/security roles.
10. Human review handoff and dashboard projections.

## Migration Strategy

### Step 1: Stabilize The Worktree

Classify the current dirty state:

- Keep committed Go runtime and docs.
- Remove uncommitted TypeScript scaffold unless explicitly archived as reference-only.
- Decide whether uncommitted Go scaffold packages are promoted, merged, or removed.
- Isolate or finish unrelated knowledge/PDF work before agency refactors.
- Remove generated root junk such as `--help` only in an explicit cleanup ticket.
- Avoid committing unrelated memory or test changes with agency work.

### Step 2: Reconcile Docs With Runtime Reality

Update agency docs to reference the current working seams:

- `cmd/odin` as current binary.
- `internal/app/lifecycle` as command/service composition.
- `internal/runtime/jobs` as current queued work runner.
- `internal/executors` as runner seam.
- `internal/vcs` as worktree isolation seam.
- `internal/store/sqlite` as authority.

Do not describe `internal/runner`, `configs/`, or TypeScript files as target architecture unless explicitly accepted.

### Step 3: Add Missing Contracts Before New Runtime Logic

Add narrow docs/contracts for:

- agency orchestrator capability
- GitHub issue intake
- delivery gates
- Codex exec security and launch policy

These should reference existing contracts instead of restating authority rules.

### Step 4: Ship One Vertical Slice At A Time

Each slice must include:

- a small package or command change
- focused unit/contract tests
- integration test when state, Git, SQLite, subprocesses, or HTTP are involved
- real `odin` command proof for user-visible behavior

### Step 5: Defer App-Server Until Codex Exec Works

Codex app-server should remain behind the executor interface and should not leak thread/turn terms into durable Odin Work Item state.

## First 10 Safe Refactor Tickets

1. **Classify and remove accidental TypeScript scaffold**
   - Remove `src/`, `package.json`, `package-lock.json`, `tsconfig.json`, `eslint.config.js`, and `tests/agency-scaffold.test.ts` after confirming no wanted artifacts remain.
   - Proof: `go test ./...` no longer lists `node_modules/flatted/golang/pkg/flatted`.

2. **Decide and document the binary entrypoint**
   - Choose either `cmd/odin` only or keep `cmd/odin-os` as a daemon alias.
   - If keeping both, document why and add command-level tests.
   - Proof: `go build ./cmd/odin` and chosen entrypoint command pass.

3. **Collapse duplicate runner abstractions**
   - Remove or merge uncommitted `internal/runner/*` into `internal/executors`.
   - Keep `internal/executors/contract` as canonical.
   - Proof: router tests and `go test ./internal/executors/...`.

4. **Collapse duplicate config roots**
   - Canonical decision: keep `config/` as the only repo-authored configuration root.
   - Keep active config loading through `internal/app/config`, `internal/app/bootstrap`, and explicit `config/*.yaml` paths; do not add a loader fallback for `configs/`.
   - Treat `configs/` as duplicate agency examples. Preserve useful fields by moving them into `config/agency.example.yaml` or an operations doc, then remove `configs/` in a cleanup PR.
   - Reference checks before removal: `rg -n "configs/" .` and `rg -n "config/agency.example.yaml|configs/(default|development|production.example).yaml" docs config configs`.
   - Rollback: revert the cleanup commit or restore the removed example files from Git. No database rollback is required because `configs/` is not runtime-loaded.
   - Proof: bootstrap/config/lifecycle tests and `ODIN_ROOT=<tmp> ./bin/odin doctor --json`.

5. **Add top-level Odin usage output**
   - Implement `odin help` and `odin --help` through `internal/app/lifecycle`.
   - Proof: real commands print usage and exit cleanly.

6. **Add one minimal delivery profile registry asset**
   - Add a `registry/workflows/*` entry tagged `delivery_profile`.
   - Use existing registry loader/validator.
   - Proof: `ODIN_ROOT=<tmp> ./bin/odin work profiles` shows the profile.

7. **Split SQLite store by domain file without changing package API**
   - Move methods into files such as `projects.go`, `tasks.go`, `runs.go`, `knowledge.go`, `leases.go` inside `internal/store/sqlite`.
   - No behavior changes.
   - Proof: `go test ./internal/store/sqlite`.

8. **Preserve GitHub intake package root**
   - Keep GitHub tracker behavior under `internal/tracker`.
   - Keep `internal/adapters/github` empty unless a later ADR assigns it a
     non-tracker GitHub responsibility.
   - Proof: package inventory has one GitHub intake seam.

9. **Add service hardening plan for systemd**
   - Update `deploy/systemd/odin.service` with reviewed hardening options after confirming user/system deployment mode.
   - Proof: documented install path and `systemd-analyze verify` where available.

10. **Add agency readiness status command slice**
   - Extend `odin work status` or add `odin work readiness` to report intake, dispatch, runner, worktree, and kill-switch readiness from existing state.
   - No worker launch yet.
   - Proof: real `odin` command output shows readiness without mutating GitHub, worktrees, or workers.

## Sequencing Notes

Tickets 1 through 4 are cleanup and seam decisions. Do them before any real agency feature work.

Tickets 5 and 6 improve the operator feedback loop without adding dangerous automation.

Tickets 7 through 10 prepare the codebase for real GitHub intake and Codex execution without introducing a sidecar orchestrator.

## Stop Conditions

Stop and re-audit if:

- a feature requires a second database
- a feature requires treating GitHub as runtime authority
- a feature requires bypassing `internal/executors`
- a feature mutates default branches
- a feature launches workers without worktree leases
- a feature needs production secrets in worker context
- a feature can only be proven through tests and not through the real `odin` command path
