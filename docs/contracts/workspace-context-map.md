---
title: Workspace Context Map
status: active
date: 2026-04-16
---

# Workspace Context Map

This document defines the target bounded contexts for Odin OS and the direction of allowed dependency flow between them. The goal is a modular-monolith center of gravity with workspace as the root, not a distributed-services architecture.

## Dependency rule

Dependencies should point inward toward the smallest context that owns the rule. Contexts that only project, adapt, or execute should depend on the semantic center, not the other way around.

## Target bounded contexts

| Context | Owns | Depends on | Primary package surfaces | Must not own |
| --- | --- | --- | --- | --- |
| `workspace` | Workspace identity, active workspace resolution, and top-level operating state. | None of the other target contexts. | `internal/core`, `internal/app` | Initiative policy, companion behavior, or execution mechanics. |
| `initiative` | Durable responsibility streams and their lifecycle. | `workspace` | `internal/core`, `internal/runtime` | Execution infrastructure or adapter-specific concerns. |
| `companion` | Durable AI roles such as assistants, advisors, operators, and specialists. | `workspace`, `initiative`, `capability catalog` | `internal/core`, `internal/workers`, `internal/registry` | Workspace policy, project governance, or execution routing. |
| `project governance` | Managed project policy, control-scope validation, branch/worktree rules, and approval gates. | `workspace`, `initiative`, `capability catalog` | `internal/core`, `internal/vcs` | General-purpose planning or adapter behavior. |
| `capability catalog` | Capability definitions, loading rules, and the lightweight catalog used by the rest of the system. | `workspace` metadata only, if needed for indexing. | `internal/tools`, `internal/registry` | Work execution policy or project governance rules. |
| `planning` | Decomposition, sequencing, lane selection, swarm admission proposals, and plan generation for work items. | `workspace`, `initiative`, `companion`, `capability catalog`, `memory`, `project governance` | `internal/core`, `internal/workers`, `internal/executors` | Direct adapter implementation or persistent runtime authority. |
| `work execution` | Execution lanes, run attempts, supervised delegations, orchestration of governed work, and retry handling. | `planning`, `project governance`, `companion`, `memory`, `integrations` | `internal/runtime`, `internal/executors`, `internal/workers` | Planning policy, catalog ownership, or workspace identity rules. |
| `memory` | Durable project, initiative, companion, work item, and run-attempt memory access and projection. | `workspace`, `initiative` | `internal/memory`, `internal/store` | Planning logic, execution routing, or adapter-specific policy. |
| `integrations` | Boundary adapters for GitHub, Git, shell, files, calendar, mail, and other external systems. | `workspace`, `project governance` | `internal/adapters`, `internal/vcs` | Domain rules, planning logic, or canonical state ownership. |

## Dependency direction

The intended dependency graph is:

- `workspace` -> none
- `initiative` -> `workspace`
- `companion` -> `workspace`, `initiative`, `capability catalog`
- `project governance` -> `workspace`, `initiative`, `capability catalog`
- `capability catalog` -> none, or at most workspace metadata for indexing
- `planning` -> `workspace`, `initiative`, `companion`, `capability catalog`, `memory`, `project governance`
- `work execution` -> `planning`, `project governance`, `companion`, `memory`, `integrations`
- `memory` -> `workspace`, `initiative`
- `integrations` -> `workspace`, `project governance`

## Practical reading

- Workspace is the semantic root and should remain the first question when resolving context.
- Initiatives organize durable responsibility streams inside a workspace.
- Companions are durable roles, not disposable task runners.
- Swarms are not a new top-level context. They are a bounded execution pattern inside work execution built from parent work items, child delegations, and normal run attempts.
- Project governance is a policy boundary on managed projects, not the whole system.
- Planning decides what should happen; work execution carries it out; memory preserves what matters; integrations connect Odin to external systems.
- Capability catalog data should stay lightweight and be loaded only when a consumer needs it.
- Integrations are one-way boundaries into external systems; work execution depends on them, but they do not depend back on execution.
