# Odin OS Brownfield Workflow

## Purpose

This workflow keeps future Odin OS work brownfield-safe. It exists to preserve working behavior while migrating the repository toward the target architecture described in `docs/brownfield/MIGRATION_PLAN.md`.

## Standard Sequence

1. **Audit**
   - Read `AGENTS.md`, this file, `CONTEXT.md`, relevant ADRs, `docs/contracts/`, and the brownfield audit docs.
   - Locate existing commands, packages, tests, configs, registry assets, scripts, deployment files, and migration notes related to the task.
   - Check the current worktree state before editing.

2. **Classify**
   - Classify touched assets as keep, refactor, replace, or remove.
   - Use `docs/brownfield/COMPONENT_INVENTORY.md` as the starting point.
   - If classification is unclear, write down the ambiguity instead of guessing.

3. **Characterize**
   - For risky or poorly understood behavior, add or identify characterization tests before refactoring.
   - Prefer tests at the existing module interface.
   - For operator behavior, prefer real `odin` command proof over internal-only tests.

4. **Refactor In Small Slices**
   - Change the smallest useful unit.
   - Preserve existing interfaces unless the ticket explicitly changes them.
   - Keep compatibility paths until a removal ticket and migration reason exist.
   - Avoid renames and moves unless they reduce a documented conflict or duplicate seam.

5. **Verify**
   - Run targeted tests first.
   - Run Go quality gates when feasible:

```bash
go fmt ./...
go vet ./...
go test ./...
go build ./cmd/odin-os
```

   - For user-visible or orchestration-facing behavior, also run a real `odin` command against a controlled runtime root.
   - For Odin-OS orchestration, tracker, runner, prompt, skill, shim, workspace, dashboard, deployment, or security changes, run `make odin-e2e-local` and do not claim completion if it fails.

6. **Document**
   - Update docs when behavior, contracts, security posture, or operator workflow changes.
   - Record architectural decisions in `docs/architecture/ADR-*.md`.
   - Summaries must separate proven behavior from unproven behavior.

## Keep / Refactor / Replace / Remove Rules

### Keep

Keep an asset when it is working, tested, documented, or still carries operational knowledge.

Use this for:

- `cmd/odin` and `internal/app/lifecycle`
- SQLite runtime authority under `internal/store/sqlite`
- registry assets under `registry/`
- real runtime modules such as jobs, recovery, health, VCS leases, and executor routing
- thin scripts that call repo-owned Odin commands

Allowed work:

- add tests
- deepen interfaces
- improve docs
- extend through existing seams

### Refactor

Refactor when behavior is useful but locality, naming, or structure is messy.

Requirements:

- preserve behavior unless the ticket says otherwise
- add characterization coverage for risky behavior
- keep changes small and reviewable
- prefer same-package moves before cross-package redesigns

Examples:

- splitting a large Go file inside the same package
- turning a placeholder into a real adapter at an existing seam
- making an operator command call a shared service instead of duplicating logic

### Replace

Replace only when an existing implementation cannot satisfy the target architecture safely.

Requirements:

- document why refactor is insufficient
- keep compatibility until callers migrate
- include a rollback or fallback plan
- record the decision in `docs/architecture/ADR-*.md`

Examples:

- replacing a deterministic alpha executor with a real executor behind the same contract
- replacing an accidental scaffold with an existing Odin runtime service

### Remove

Remove only when the asset is inventoried as duplicate, generated, obsolete, or explicitly approved for removal.

Requirements:

- inventory the asset first
- identify callers or prove there are none
- preserve useful knowledge in docs or migration notes
- include tests or command checks proving behavior still works

Examples:

- removing accidental TypeScript scaffold files after confirming Go-native Odin is the target
- removing duplicate runner/config roots after the canonical seam is documented

## Duplicate Seam Policy

Do not add a new package, command group, runner, registry, config root, or shim until you have checked for an existing seam.

Known canonical seams:

- command/service composition: `internal/app/lifecycle`
- operator commands and REPL: `internal/cli`
- runtime state: `internal/store/sqlite`
- executor runners: `internal/executors`
- work isolation: `internal/vcs`
- authored registry: `registry`
- project governance: `internal/core/projects`
- operational HTTP: `internal/api/http`

Known duplicate or unresolved seams from the audit:

- `cmd/odin` vs `cmd/odin-os`
- `internal/executors` vs `internal/runner`
- `config/` vs `configs/`
- `internal/adapters/github` vs `internal/tracker/github`
- Go-native Odin vs accidental TypeScript scaffold under `src/`

Resolve these by migration tickets, not by adding a third path.

## Security Review

Add a security review whenever a change touches:

- runners, executors, or app-server integration
- shell execution, subprocesses, shims, or scripts
- filesystem mutation, cleanup, worktrees, backups, or restore
- GitHub tokens, issue mutation, labels, comments, PRs, or repo permissions
- secrets, credentials, environment files, or deployment files
- worker sandboxing or command policy

The review must confirm:

- no production secrets are exposed to workers
- no direct commit to default branch is introduced
- no autonomous merge or production deploy is introduced
- process execution and filesystem mutation stay inside approved worktrees or runtime roots

## When To Stop

Stop and ask for a decision or create an explicit blocker when:

- the task requires a second runtime authority
- a change would bypass SQLite runtime state
- a change would bypass `internal/executors` for worker execution
- a change would treat GitHub as runtime truth
- a change would delete an existing skill, agent, shim, or registry asset without approval
- tests would need to be weakened to pass
- real `odin` proof is required but cannot be produced
