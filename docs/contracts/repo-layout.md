---
title: Repository Layout Contract
status: active
date: 2026-04-16
phase: "00"
---

# Repository Layout Contract

This document defines the target package and folder boundaries for the new Odin OS repository. The semantic center is workspace, initiative, companion, project governance, capability catalog, planning, work execution, memory, and integrations. The purpose is to keep responsibilities non-overlapping and to prevent future phases from smearing authored assets, runtime state, adapters, and projections together.

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

## Semantic center

The domain should read from top to bottom as:

`workspace` -> `initiative` -> `companion` -> `planning` -> `work execution`

`project governance`, `capability catalog`, `memory`, and `integrations` support that flow without becoming the center themselves.

Package alignment:

- `capability catalog` maps primarily to `internal/tools`, with authored definitions living in `registry/`.
- `integrations` maps primarily to `internal/adapters` and `internal/vcs`.
- `work execution` maps primarily to `internal/runtime`, `internal/executors`, and the execution-oriented parts of `internal/workers`.
- `planning` and `project governance` remain centered in `internal/core`.
- `memory` maps primarily to `internal/memory`, with persistence in `internal/store`.

## Internal package boundaries

### `internal/app`

Owns bootstrap, lifecycle wiring, and configuration loading. It composes the system but should not absorb domain logic that belongs deeper in the tree.

### `internal/cli`

Owns REPL, CLI entrypoints, TUI surfaces, rendering, and explicit control-scope presentation. It is the operator interface layer and should consume projections and orchestration services rather than implement them.

### `internal/api`

Owns HTTP and WebSocket transport. It should expose the same underlying orchestration capabilities as the CLI, not a separate control plane with divergent logic.

### `internal/core`

Owns the semantic-center contracts: workspace resolution, initiative lifecycle, companion assignment, planning rules, work execution rules, and project governance. This is the domain center and must not depend directly on provider-specific adapters.

### `internal/runtime`

Owns jobs, runs, execution lanes, run attempts, events, projections, health, recovery, uncertainty handling, and checkpoints. Runtime packages model what happens while Odin is operating and how it recovers or compacts context.

### `internal/registry`

Owns loading, parsing, validating, compiling, and watching authored registry assets. It translates Markdown-frontmatter content into runtime-usable forms but does not own the authored source itself.

### `internal/skills`

Owns canonical skill CRUD, rendering, reference checks, and runtime invocation against the registry-backed skill contract. It is the single lifecycle layer for skill maintenance and execution and should be reused by CLI, broker, and Codex-facing workflows.

### `internal/learning`

Owns evaluators, proposals, promotion, and replay. It is the bounded self-improvement subsystem and must remain proposal-driven and reversible.

### `internal/memory`

Owns runtime services for workspace, initiative, companion, work item, run attempt, and knowledge memory access. It should index and project canonical authored memory and runtime-derived knowledge without becoming a second registry.

### `internal/workers`

Owns planner, builder, reviewer, QA, and research worker roles. Workers coordinate execution behavior, but they should use shared contracts for tools, executors, policy, and runtime state.

### `internal/executors`

Owns the common executor contract, routing, execution-lane handling, and backend implementations such as Codex, Claude Code, Gemini CLI, and API-backed executors. Backend-specific code belongs here, never in `core` or `workers`.

### `internal/tools`

Owns broker access, capability catalogs, invocation, and budgets. It is the runtime surface for the capability catalog context, while the authored catalog definitions remain in `registry/`. Tool loading must be dynamic and control-scope-aware; this package exists to make that explicit.

### `internal/vcs`

Owns Git integration, worktrees, branches, and leases. All mutating project work should flow through these packages rather than shelling out ad hoc from unrelated modules.

### `internal/adapters`

Owns boundary integrations such as shell, filesystem, web, Gmail, and calendar. Adapters translate outside systems into internal contracts; they do not own domain rules. Git-specific operational behavior that is not domain policy belongs here only when it is an external-system adapter concern.

GitHub issue and pull request tracker behavior is the current exception: it
belongs under the canonical `internal/tracker` seam. The
`internal/adapters/github` directory is reserved empty and must not receive
issue, pull request, label, comment, token, follow-up issue, or tracker
behavior unless a later ADR explicitly moves that responsibility.

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

## Vocabulary guardrails

- Use `workspace`, `initiative`, `companion`, `work item`, `run attempt`, `control scope`, and `execution lane` in new architecture discussions.
- Treat `task`, `agent`, `command`, and `scope` as narrowed terms unless the legacy mapping is the point of the document.
