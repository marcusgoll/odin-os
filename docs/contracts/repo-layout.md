---
title: Repository Layout Contract
status: active
date: 2026-04-08
phase: "00"
---

# Repository Layout Contract

This document defines the target package and folder boundaries for the new Odin OS repository. The purpose is to keep responsibilities non-overlapping and to prevent future phases from smearing authored assets, runtime state, adapters, and projections together.

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
| `prompts/` | Canonical prompt assets | mutable run output |
| `memory/` | Canonical authored durable memory docs | transient execution state |
| `config/` | Operator-authored configuration and manifests | live mutable runtime truth |
| `data/` | Canonical local runtime database and other durable local data stores | human-authored contracts |
| `runs/` | Run artifacts, logs, and summaries | source of truth |
| `state/` | Rebuildable caches, compiled assets, snapshots | canonical authored data or canonical runtime authority |
| `docs/` | ADRs, contracts, migration notes, and operations documentation | runtime implementation |
| `scripts/` | Development, CI, and migration helpers | hidden application runtime |
| `tests/` | Unit, integration, and replay tests | production code |

## Internal package boundaries

### `internal/app`

Owns bootstrap, lifecycle wiring, and configuration loading. It composes the system but should not absorb domain logic that belongs deeper in the tree.

### `internal/cli`

Owns REPL, command entrypoints, TUI surfaces, rendering, and explicit scope presentation. It is the operator interface layer and should consume projections and orchestration services rather than implement them.

### `internal/api`

Owns HTTP and WebSocket transport. It should expose the same underlying orchestration capabilities as the CLI, not a separate control plane with divergent logic.

### `internal/core`

Owns intake, routing, context management, approvals, policy, scheduling, orchestration, and project governance rules. This is the domain center and must not depend directly on provider-specific adapters.

### `internal/runtime`

Owns jobs, runs, events, projections, health, recovery, uncertainty handling, and checkpoints. Runtime packages model what happens while Odin is operating and how it recovers or compacts context.

### `internal/registry`

Owns loading, parsing, validating, compiling, and watching authored registry assets. It translates Markdown-frontmatter content into runtime-usable forms but does not own the authored source itself.

### `internal/learning`

Owns evaluators, proposals, promotion, and replay. It is the bounded self-improvement subsystem and must remain proposal-driven and reversible.

### `internal/memory`

Owns runtime services for user, project, run, and knowledge memory access. It should index and project canonical authored memory and runtime-derived knowledge without becoming a second registry.

### `internal/workers`

Owns planner, builder, reviewer, QA, and research worker roles. Workers coordinate execution behavior, but they should use shared contracts for tools, executors, policy, and runtime state.

### `internal/executors`

Owns the common executor contract, routing, and backend implementations such as Codex, Claude Code, Gemini CLI, and API-backed executors. Backend-specific code belongs here, never in `core` or `workers`.

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
- `adapters/` may depend inward on contracts, never outward on CLI, TUI, or specific worker roles.
- `executors/` expose a shared contract; plan-backed headless runners fit here only if they satisfy that same contract.
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
