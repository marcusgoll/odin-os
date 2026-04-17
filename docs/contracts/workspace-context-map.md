---
title: Workspace Context Map
status: active
date: 2026-04-17
phase: "17"
---

# Workspace Context Map

This contract defines the current runtime ownership map for workspace-era memory in `odin-os`.

## Canonical Ownership

- `odin-os` is the only canonical runtime owner.
- `odin-orchestrator` remains migration input only.
- Mutable runtime memory lives in SQLite.
- Authored files under `memory/` remain reviewed source material when explicitly imported or referenced; they are not the mutable runtime authority.

## Runtime Memory Subjects

### Workspace

The default Marcus workspace is the top-level owner for durable runtime memory that spans initiatives and companions.

Workspace-owned memory includes:

- durable Marcus-wide preferences
- standing constraints
- cross-initiative defaults
- workspace-wide summaries

### Initiative

Initiatives are workspace-owned runtime subjects that localize memory for one managed responsibility.

Initiative-owned memory includes:

- managed-project summaries
- local operating context
- initiative-specific durable facts
- project-linked memory lineage when an initiative is backed by a runtime `project`

### Companion

Companions are workspace-owned runtime roles. Companion memory is an overlay, not an independent truth silo.

Companion-owned memory includes:

- role-local overlay notes
- scoped durable companion summaries when explicitly recorded

Companion memory must not silently replace workspace or initiative truth.

### Run

Runs preserve execution lineage and episodic context.

Run-linked memory includes:

- conversation transcripts
- explicit episode summaries
- `task_id`, `run_id`, and `source_transcript_id` lineage

## Current Storage Model

Workspace-era memory now flows through:

- `workspaces`
- `initiatives`
- `companions`
- `conversation_transcripts`
- `memory_summaries`

The runtime currently records explicit ownership through:

- `workspace_id`
- `initiative_id`
- `companion_id`
- existing lineage through `project_id`, `task_id`, `run_id`, and `source_transcript_id`

`scope` and `scope_key` remain transitional compatibility fields and must not be treated as the semantic center of the model.

## Current Operator Surfaces

The runtime currently exposes read-only scoped memory views through:

- REPL `/memory`
- ask-mode memory questions
- operational HTTP `/memoryz`
- scoped projection queries under `internal/runtime/projections`

These surfaces are intentionally bounded and scoped to the canonical Marcus workspace.

## Deferred Work

The following are not part of the current cutover:

- retrieval ranking beyond bounded scoped queries
- lifecycle rotation and archive jobs
- candidate-memory promotion pipelines
- operator mutation tooling for correction or deletion
- orchestrator-style generated hot-memory projections

Those capabilities may be added later, but they must extend this ownership model rather than bypass it.
