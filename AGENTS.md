# Odin OS Agent Instructions

## Scope And Precedence

This file applies to work inside this `odin-os` repository.

Instruction precedence for Codex workers:

1. System and developer instructions from the current Codex session.
2. Repo-local `AGENTS.md` in this directory.
3. Parent `/home/orchestrator/AGENTS.md` when working from this machine.
4. `WORKFLOW.md`.
5. `CONTEXT.md`, `docs/adr/`, `docs/architecture/ADR-*.md`, `docs/contracts/`, and task-specific plans.

If these sources conflict, preserve safety and existing runtime behavior first, then stop and document the conflict before editing. The parent `AGENTS.md` requirement still applies: audit first, reuse existing Odin structures, and verify operator-visible behavior through the real `odin` command path where applicable.

## Brownfield Operating Rules

Odin OS is a brownfield Go orchestration system, not a greenfield app. Treat existing commands, services, contracts, registries, schemas, docs, tests, scripts, agents, skills, shims, and deployment files as potentially valuable until proven otherwise.

Before editing:

1. Read the relevant current docs, especially `docs/brownfield/AUDIT.md`, `docs/brownfield/COMPONENT_INVENTORY.md`, `docs/brownfield/MIGRATION_PLAN.md`, `CONTEXT.md`, accepted ADRs, and related contracts.
2. Identify existing commands, services, contracts, registries, schemas, docs, tests, scripts, configs, runners, shims, and operator surfaces for the requested change.
3. Summarize what exists, what is partial, what is missing, and which existing modules will be reused.

Default decisions:

- Prefer modifying or deepening existing modules over creating duplicate modules.
- Prefer thin adapters over parallel systems.
- Do not create new shims unless no existing integration point works.
- Do not rename, move, or delete files without a migration reason.
- Do not delete existing skills, agents, shims, scripts, registry assets, or legacy migration references without inventory and explicit approval.
- Add characterization tests before refactoring risky behavior.
- Keep refactors small, reviewable, and reversible.
- Maintain backward compatibility unless a ticket explicitly removes it.
- Keep `cmd/odin`, `internal/app/lifecycle`, `internal/store/sqlite`, `internal/executors`, `internal/vcs`, `registry`, and top-level `odin ...` proofs as the default architectural center unless an ADR says otherwise.

## Keep / Refactor / Replace / Remove

Use the brownfield classifications from `docs/brownfield/COMPONENT_INVENTORY.md`:

- **Keep**: preserve behavior and extend through existing interfaces.
- **Refactor**: improve locality or clarity while preserving behavior.
- **Replace**: supersede an implementation only after a migration path and compatibility plan exist.
- **Remove**: delete only when an asset is inventoried as duplicate, generated, obsolete, or explicitly approved for removal.

Any classification change should be reflected in the brownfield docs or a follow-up ticket.

## Architecture Decisions

Record every new architectural decision in `docs/architecture/ADR-*.md`.

Use an ADR when a decision:

- changes package ownership or command authority
- adds or removes a runner, shim, adapter, or service boundary
- changes state authority, filesystem layout, worktree behavior, process execution, or deployment behavior
- changes security posture, GitHub token usage, secret handling, or worker sandboxing
- removes backward compatibility

Existing `docs/adr/` files remain accepted historical ADRs. New brownfield migration decisions should use `docs/architecture/ADR-*.md` unless a task explicitly says to update the older ADR set.

## Skills, Agents, And Shims

In-repo registry assets under `registry/agents/`, `registry/skills/`, and `registry/workflows/` are Odin-authored assets. Legacy skill and shim references in `docs/migration/legacy-inventory.md` are migration evidence, not active runtime authority.

If an external Codex skill, migrated skill, registry agent, worker package, or shim conflicts with implemented Odin behavior:

1. Treat implemented Odin behavior and accepted ADRs as current truth.
2. Treat docs and skills as intent or migration evidence.
3. Document the conflict before changing code.
4. Do not resolve by adding a parallel command group, registry, runner, or shim.

## Security Review Triggers

Any change touching the following requires an explicit security review section in the task summary or PR:

- runners or executors
- shims, shell wrappers, or process execution
- filesystem mutation, cleanup, backup, restore, or worktree operations
- GitHub tokens, GitHub APIs, pull requests, labels, or issue mutation
- secrets, credentials, environment variables, or deployment configuration
- worker sandboxing, approval policy, or command allowlists

Security-sensitive changes must state what was proven, what remains unproven, and whether production secrets or production deployment paths were untouched.

## Required Verification

For Go changes, run the relevant targeted tests plus the standard quality gates when feasible:

```bash
go fmt ./...
go vet ./...
go test ./...
go build ./cmd/odin-os
```

For user-visible or orchestration-facing behavior, also build or use the repo-owned `odin` binary and exercise the real command path with a controlled `ODIN_ROOT`.

If a gate cannot be run, record why and what risk remains.

## Required Report Format

Every Odin implementation update must include:

- Current State
- What Already Exists
- Gaps
- Reuse Plan
- New Additions
- Why New Additions Are Necessary
- Real odin E2E Verification
- Remaining Risks
- Best operating rule going forward
