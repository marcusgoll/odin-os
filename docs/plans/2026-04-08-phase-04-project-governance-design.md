---
title: Phase 04 Project Governance Design
status: accepted
date: 2026-04-08
phase: "04"
---

# Phase 04 Project Governance Design

## Goal

Introduce the authored project governance contract for Odin OS so the runtime can classify managed projects, validate their policy boundaries, and resolve operator scope deterministically before deeper orchestration work lands.

## Chosen Approach

Phase 04 will use a centralized manifest in `config/projects.yaml` plus typed validation and scope resolution in Go.

This keeps the authored source of truth aligned with [ADR 0001](/home/orchestrator/odin-os/docs/adr/0001-canonical-authority.md), avoids introducing a second project authority, and keeps the phase limited to contracts and deterministic resolution rules. SQLite import and project transition workflows remain later-phase work.

## Rejected Alternatives

### Manifest import as the primary read path

Rejected for this phase because it would couple authored governance contracts to runtime persistence before the schema and enrollment lifecycle are stable.

### Per-project manifest files

Rejected because the current architecture authority names `config/projects.yaml` as the authored enrollment contract and because distributed manifest discovery adds complexity without earning enough value yet.

## Manifest Model

`config/projects.yaml` will define a list of managed project manifests. Each manifest will include:

- identity: `key`, `name`, `project_class`
- Git governance: `git_root`, `default_branch`
- optional GitHub metadata: `github.repo`
- system flagging: `system_project`
- authored policy: `policy`

Supported `project_class` values:

- `local_git_project`
- `github_backed_project`
- `system_project`

`system_project` is reserved for Odin self-governance and must resolve to the `odin-core` project key.

## Policy Model

Each project manifest will include a `policy` block with these authored fields:

- `allowed_commands`
- `branch_rules`
- `approval_gates`
- `merge_policy`
- `destructive_operations`

The policy model is intentionally explicit rather than generic. The validator should reject incomplete or contradictory policy definitions instead of guessing defaults for sensitive behavior.

### Required safety rules

- Every managed project must point at a Git repository.
- `github_backed_project` must declare `github.repo`.
- `system_project` must use key `odin-core` and set `system_project: true`.
- `system_project` cannot allow direct mutation of its default branch.
- Destructive operations must declare whether they are allowed and whether explicit approval is required.
- Approval gates must be authored explicitly for governance-sensitive mutations.

## Scope Model

Phase 04 will add an explicit CLI scope model in `internal/cli/scope`.

Canonical scope kinds:

- `global`
- `odin-core`
- `project`
- `new-project`

Resolution rules:

1. An explicit command target wins.
2. New-project flows resolve only to `new-project`.
3. A selected `system_project` resolves to `odin-core`.
4. A selected non-system managed project resolves to `project`.
5. Otherwise the CLI remains in `global`.

The current working directory may be used as a hint for warnings or suggestions, but not as the authoritative scope source.

## Package Ownership

### `internal/core/projects`

Owns:

- typed manifest structs
- YAML loading
- manifest validation
- project registration results

### `internal/cli/scope`

Owns:

- scope kinds
- deterministic scope resolution
- scope presentation helpers for later CLI work

### `docs/contracts`

Owns:

- manifest contract documentation
- scope model documentation

## Testing Strategy

Phase 04 should use table-driven tests for both valid and invalid authored manifests plus deterministic scope resolution tests.

Manifest tests should cover:

- valid local Git project
- valid GitHub-backed project
- valid `odin-core` system project
- duplicate keys
- missing Git roots
- non-Git directories
- missing GitHub repo metadata
- unsafe system-project branch policy
- incomplete destructive-operation and approval-gate policies

Scope tests should cover:

- `global` default state
- `odin-core` selection
- normal project selection
- `new-project` selection
- precedence of explicit targets over hints

## Phase Boundary

Phase 04 introduces authored governance contracts and scope resolution only.

It does not yet implement:

- SQLite import of project manifests
- branch or worktree creation
- transition or cutover workflows
- CLI command execution behavior beyond scope classification
