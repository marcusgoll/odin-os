# Odin OS Agent Rules

## Canonical Repo

Work inside `odin-os`.

`odin-orchestrator` is a migration source only and is being phased out. Do not treat it as the primary implementation target or runtime root unless the user explicitly asks for legacy migration work.

## Command Policy

- Use `odin ...` for all operator-facing commands and runtime proofs.
- Before command execution, verify `which odin` resolves to the intended binary.
- Use `./bin/odin ...` only for explicit repo-local build verification after a fresh build.

## Reuse and Verification Rules

Before implementing any Odin feature, Codex must first audit the existing repo state.

Instruction precedence for Codex workers:

1. System and developer instructions from the current Codex session.
2. Repo-local `AGENTS.md` in this directory.
3. Parent `/home/orchestrator/AGENTS.md` when working from this machine.
4. `WORKFLOW.md`.
5. `CONTEXT.md`, `docs/adr/`, `docs/architecture/ADR-*.md`, `docs/contracts/`, and task-specific plans.

If these sources conflict, preserve safety and existing runtime behavior first, then stop and document the conflict before editing.

Required steps:

1. Identify existing commands, services, contracts, registries, schemas, docs, and tests related to the requested feature.
2. Summarize what already exists, what is partial, and what is missing.
3. Prefer extending or repairing existing Odin structures over creating new parallel ones.
4. Do not introduce duplicate abstractions, overlapping command groups, or redundant registries without explicit justification.
5. If a repo-owned `odin` command exists for the target behavior, use it for verification.
6. Verification is incomplete unless the real `odin` command path is exercised where applicable.

Brownfield defaults:

- Prefer modifying or deepening existing modules over creating duplicate modules.
- Prefer thin adapters over parallel systems.
- Do not create new shims unless no existing integration point works.
- Do not rename, move, or delete files without a migration reason.
- Do not delete existing skills, agents, shims, scripts, registry assets, or legacy migration references without inventory and explicit approval.
- Add characterization tests before refactoring risky behavior.
- Maintain backward compatibility unless a ticket explicitly removes it.
- Keep `cmd/odin`, `internal/app/lifecycle`, `internal/store/sqlite`, `internal/executors`, `internal/vcs`, `registry`, and top-level `odin ...` proofs as the default architectural center unless an ADR says otherwise.

Security-sensitive changes touching runners, executors, shims, shell wrappers, process execution, filesystem mutation, worktree cleanup, GitHub tokens, GitHub APIs, secrets, deployment, worker sandboxing, approval policy, or command allowlists must include an explicit security review section.

Every implementation output must include:

- Existing state found
- Reused components
- New components added
- Why new components were necessary
- Real `odin` command E2E checks performed

## Odin Task Report Format

Every Odin phase or task report must include:

- Current State
- What Already Exists
- Gaps
- Reuse Plan
- New Additions
- Why New Additions Are Necessary
- Real odin E2E Verification
- Remaining Risks
- Best operating rule going forward

## Required Sequence

For Odin tasks, follow this sequence:

1. Audit
2. Verify
3. Reuse
4. Refactor if needed
5. Create only what is missing
6. Prove it through real `odin` commands
