---
title: Repository Layout Contract
status: active
date: 2026-04-16
phase: "00"
---

# Repository Layout Contract

This document defines the target package and folder boundaries for the new Odin OS repository. The purpose is to keep responsibilities non-overlapping and to prevent future phases from smearing authored assets, runtime state, adapters, and supporting read models together.

The semantic center of the repo is Odin's control plane for one primary workspace. Durable product objects are `workspace`, `initiative`, `companion`, `policy`, `memory`, `work item`, and `run attempt`. Package boundaries should reinforce that center instead of creating a second architecture around workers, executors, or managed projects.

## Top-level layout

```text
odin-os/
  cmd/
  internal/
  registry/
  prompts/
  memory/
  config/
  data/
  runs/
  state/
  docs/
  scripts/
  tests/
```

## Top-level folder responsibilities

| Path | Responsibility | Must not become |
| --- | --- | --- |
| `cmd/` | Process entrypoints only | shared business logic |
| `internal/` | Runtime implementation packages | operator-authored registry or memory assets |
| `registry/` | Canonical authored registry definitions | compiled cache or runtime state |
| `prompts/` | Canonical prompt assets | mutable run attempt output |
| `memory/` | Canonical authored durable memory docs | transient execution state |
| `config/` | Operator-authored configuration and manifests | live mutable runtime truth |
| `data/` | Canonical local runtime database and other durable local data stores | human-authored contracts |
| `runs/` | Run attempt artifacts, logs, and summaries | source of truth |
| `state/` | Rebuildable caches, compiled assets, snapshots | canonical authored data or canonical runtime authority |
| `docs/` | ADRs, contracts, migration notes, and operations documentation | runtime implementation |
| `scripts/` | Development, CI, and migration helpers | hidden application runtime |
| `tests/` | Unit, integration, and replay tests | production code |

## Internal package boundaries

### `internal/app`

Owns bootstrap, lifecycle wiring, and configuration loading. It composes the system but should not absorb domain logic that belongs deeper in the tree.

### `internal/cli`

Owns REPL, command entrypoints, TUI surfaces, rendering, and operator-facing context presentation. It is the operator interface layer and should consume read models and orchestration services rather than implement them.

### `internal/api`

Owns HTTP and WebSocket transport. It should expose the same underlying orchestration capabilities as the CLI, not a separate control plane with divergent logic.

### `internal/core`

Owns the control-plane domain model: workspace, initiative, companion, policy, approvals, scheduling, orchestration, work-item lifecycle, and project governance rules. This is the domain center and must not depend directly on provider-specific adapters.

### `internal/runtime`

Owns jobs, run attempts, events, health, recovery, uncertainty handling, and the implementation of projections and checkpoints that support control-plane recovery and views. Runtime packages model what happens while Odin is operating after control-plane decisions have been made.

### `internal/registry`

Owns loading, parsing, validating, compiling, and watching authored registry assets. It translates Markdown-frontmatter content into runtime-usable forms but does not own the authored source itself.

### `internal/skills`

Owns canonical skill CRUD, rendering, reference checks, and runtime invocation against the registry-backed skill contract. It is the single lifecycle layer for skill maintenance and execution and should be reused by CLI, broker, and Codex-facing workflows.

### `internal/learning`

Owns evaluators, proposals, promotion, and replay. It is the bounded self-improvement subsystem and must remain proposal-driven and reversible.

### `internal/memory`

Owns runtime services for workspace, initiative, companion, project, run attempt, and knowledge memory access. It should index and project canonical authored memory and runtime-derived knowledge without becoming a second registry.

### `internal/workers`

Owns planner, builder, reviewer, QA, and research worker roles in the execution plane. Workers advance bounded work items, but they should use shared contracts for tools, executors, policy, and runtime state instead of owning durable product truth.

### `internal/executors`

Owns the common executor contract, routing, and backend implementations such as Codex, Claude Code, Gemini CLI, and API-backed executors. Executors are replaceable execution lanes; backend-specific code belongs here, never in `core` or `workers`.

### `internal/tools`

Owns broker access, tool catalogs, invocation, and budgets. Tool loading must be dynamic and scoped; this package exists to make that explicit.

### `internal/vcs`

Owns Git integration, worktrees, branches, and leases. All mutating project work should flow through these packages rather than shelling out ad hoc from unrelated modules.

### `internal/adapters`

Owns boundary integrations such as GitHub, shell, filesystem, web, Gmail, and calendar. Adapters translate outside systems into internal contracts; they do not own domain rules.

### `internal/store`

Owns storage implementations, beginning with SQLite. Domain packages depend on storage interfaces or services, not the reverse.

### `internal/telemetry`

Owns structured logs, metrics, traces, and audit delivery. Telemetry consumes events and state changes; it does not become a second runtime authority.

## Boundary rules

- `core/` may depend on contracts and services, but not on transport-specific CLI or API packages.
- `core/` owns durable control-plane semantics; `runtime/`, `workers/`, and `executors/` must not redefine workspace, initiative, companion, policy, memory, or work-item truth.
- `runtime/` may implement projections, checkpoints, and recovery mechanics, but those services remain subordinate to control-plane authority.
- `adapters/` may depend inward on contracts, never outward on CLI, TUI, or specific worker roles.
- `executors/` expose a shared contract; plan-backed headless runners fit here only if they satisfy that same contract.
- `workers/` and `executors/` make up the execution plane and operate on bounded assignments returned by the control plane.
- `registry/`, `prompts/`, and `memory/` are authored sources; compiled or indexed forms belong in `state/` or SQLite.
- `runs/` and `state/` are always disposable or reconstructable relative to canonical authorities unless an ADR explicitly promotes a subset.

## Governance-sensitive areas

The following areas are governance-sensitive and should receive stricter review and audit attention:

- `config/`
- `registry/`
- `prompts/`
- `memory/`
- `internal/core/`
- `internal/runtime/`
- `internal/executors/`
- `internal/vcs/`
- `internal/learning/`

Changes in those areas affect policy, authority, orchestration, or self-governance and should be treated accordingly.
