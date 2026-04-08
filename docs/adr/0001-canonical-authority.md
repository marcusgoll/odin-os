---
title: ADR 0001 - Canonical Authority
status: accepted
date: 2026-04-08
phase: "00"
---

# ADR 0001: Canonical Authority

## Context

The new Odin OS repository needs one clear authority model before any runtime packages are built. The previous `odin-orchestrator` repository contains useful ideas and assets, but it is a migration source, not the runtime root. Without an explicit authority model, later phases would drift into mixed ownership between filesystem state, runtime memory, legacy code, and generated artifacts.

Odin must support:

- a Go-first runtime and CLI-first operator surface
- SQLite as the initial canonical runtime store
- Markdown with frontmatter for authored assets
- managed local Git projects and optional GitHub-backed projects
- the Odin repository itself as a reserved system project
- scope-aware interactive behavior
- deterministic, auditable automation

## Decision

### 1. One canonical runtime authority

`data/odin.db`, owned through `internal/store/sqlite`, is the only canonical runtime authority.

Any state that changes while Odin runs must either:

- be committed to SQLite, or
- be explicitly treated as disposable cache or derived output

No filesystem cache, in-memory map, JSON artifact, or rendered view may outrank the SQLite runtime state.

### 2. Canonical sources of truth by concern

| Concern | Canonical authority | Why | Derived outputs |
| --- | --- | --- | --- |
| Runtime state | `data/odin.db` | Single transactional authority for runs, approvals, leases, health, checkpoints, and projections | `state/cache/`, `state/snapshots/`, `runs/` |
| Registry assets | Markdown with frontmatter under `registry/` | Human-authored, reviewable definitions for agents, skills, workflows, and commands | `state/compiled/`, in-memory indexes |
| Audit events | Append-only event records in SQLite | Events must be queryable, replayable, and transactionally related to runtime changes | structured logs in `runs/logs/`, telemetry sinks |
| Operator projections | Projection tables and views in SQLite | CLI, TUI, and API should read stable projections rather than reconstruct raw events on every request | rendered summaries in `runs/summaries/`, API responses |
| Project manifests | `config/projects.yaml` | Operator-authored enrollment and governance metadata belongs in reviewed config, not mutable runtime state | imported project rows in SQLite |
| Executor routing | `config/executors.yaml` plus `config/models.yaml` | Routing policy and capability metadata are authored configuration, while runtime decisions are recorded separately | route selections, budgets, and run metadata in SQLite |
| Durable authored memory | Markdown with frontmatter under `memory/` | User and project memory should be reviewable and portable as authored documents | indexed search state and projections in SQLite |

### 3. Scope is explicit and mandatory

Odin must always know and show its current scope. The canonical scope classes are:

- `global`
- `odin-core`
- `managed-project`
- `new-project-setup`

Interactive CLI surfaces, API requests, worker dispatch, approvals, and audit events must carry scope explicitly. Scope is not inferred only from the current working directory or a guessed repository.

### 4. Project governance rules

Managed project support follows these rules:

- Every managed project must be a Git repository.
- GitHub metadata is optional and additive; it does not replace Git as the governance baseline.
- Each managed project must have a manifest entry in `config/projects.yaml`.
- Runtime state for projects is recorded in SQLite, but the authored enrollment contract remains the manifest file.
- Mutating work for a managed project must execute in a task-owned worktree and branch, with leases recorded in runtime state.

### 5. Odin self-governance

The current repository is a reserved system project named `odin-core`.

`odin-core` follows the same baseline governance primitives as any other managed project, plus stricter constraints:

- self-modifying work must run in isolated worktrees and task-owned branches
- policy, prompt, registry, and self-improvement changes must be auditable
- self-improvement proposals must be replay-tested before promotion
- no uncontrolled self-modification is allowed

### 6. Architectural commitments required by later phases

The following are architectural commitments, not optional implementation details:

- Executors are model-agnostic and must sit behind a shared executor contract.
- Plan-backed headless CLI runners are allowed only through that common contract.
- Tools, skills, and sub-agents are loaded dynamically for the active scope and task budget.
- Context compaction and wake-packet handoffs are runtime features backed by checkpoints and durable state, not ad hoc prompt tricks.
- Broker routes and APIs remain valid unattended execution lanes and resilience paths.

## Consequences

### Positive

- Later phases have one unambiguous runtime authority.
- Authored content remains reviewable in Git while runtime mutation stays transactional.
- Project governance is uniform across local-only, GitHub-backed, and Odin self-governed work.
- Dynamic loading and bounded execution become first-class design constraints instead of later optimizations.

### Negative

- Some information exists in both authored files and SQLite, so boundaries must be enforced carefully.
- Operator-facing exports in `runs/` and `state/` cannot be treated as authoritative, even when convenient.
- Any future shortcut that stores mutable runtime truth in YAML, JSON, or memory alone violates this ADR.

## Rejected alternatives

### Filesystem-first runtime state

Rejected because concurrent mutation, replay, leases, approvals, and projections are harder to audit and reconcile than in a transactional SQLite store.

### In-memory authority with periodic dumps

Rejected because crash recovery, wake-packet handoff, and unattended execution require durable authority during the run, not only after it.

### GitHub as required project authority

Rejected because Odin must work with local Git projects as a first-class case and GitHub is optional by design.
