---
title: Odin OS Workspace Memory Port Design
status: proposed
date: 2026-04-17
source_repo: odin-orchestrator
---

# Odin OS Workspace Memory Port Design

## Goal

Move the useful parts of the workspace-centered memory architecture from the legacy `odin-orchestrator` line into `odin-os`, where they belong canonically, without copying the old runtime model wholesale.

The target is not "make `odin-os` look like `odin-orchestrator`." The target is to extend the existing `odin-os` authority model so workspace, initiative, companion, and run memory become first-class runtime concepts under the current SQLite-first design.

## Why This Is Needed

`odin-orchestrator` now contains a more complete workspace-centered memory implementation, but `odin-os` is the canonical runtime root and explicitly treats `odin-orchestrator` as a migration source only.

`odin-os` already has several relevant building blocks:

- SQLite-backed conversation transcripts and memory summaries
- runtime memory services split by user, project, run, and general knowledge
- runtime events around transcripts and memory summaries
- a proposed workspace-centered context model and a refactor plan that already calls for scoped memory

What `odin-os` still lacks is the ownership model needed for durable workspace memory:

- no `workspaces`, `initiatives`, or `companions` persistence yet
- no explicit `workspace_id`, `initiative_id`, or `companion_id` on memory rows
- no clear distinction between workspace memory, initiative memory, companion overlays, and run lineage
- no scoped operator views over those subjects

## Existing State

The current `odin-os` memory shape is already useful and should be extended rather than replaced.

### What already exists

- `internal/store/sqlite/migrations/0010_memory_and_conversations.sql`
- `internal/store/sqlite/store.go`
- `internal/store/sqlite/models.go`
- `internal/memory/users`
- `internal/memory/projects`
- `internal/memory/runs`
- `internal/memory/knowledge`

These components already provide:

- persisted transcripts
- persisted memory summaries
- lineage through `project_id`, `task_id`, `run_id`, and `source_transcript_id`
- runtime events for transcript and memory writes

### What is partial

- `docs/contracts/workspace-context-map.md` already names workspace, initiative, companion, and scoped memory as the semantic target
- `docs/plans/2026-04-16-odin-user-adaptive-workspace-refactor.md` already reserves Task 8 for scoped memory and later tasks for workspace-facing CLI and API surfaces

### What is missing

- owner entities for workspace-era memory
- first-class ownership fields on memory rows
- visibility and retention policy fields
- a migration path from generic `scope/scope_key` usage to explicit workspace-era semantics

## Design Options

### 1. Port full orchestrator parity immediately

Bring over the whole orchestrator memory stack now: retrieval bundles, lifecycle jobs, correction tooling, observability surfaces, and generated read models.

Pros:

- fastest path to feature parity
- lowest short-term gap between repos

Cons:

- high rewrite risk because `odin-os` does not yet have the owner entities this behavior assumes
- encourages cargo-cult migration from the legacy repo
- risks hard-coding orchestrator-specific operational behavior into the new runtime

### 2. Land scoped-memory foundations first, then layer behavior

Extend `odin-os` with explicit workspace-era ownership, adapt current services to that model, and only then add richer retrieval, lifecycle, and operator behavior.

Pros:

- fits the current `odin-os` authority model
- reuses real package seams already present
- keeps migration pressure on contracts instead of compatibility tricks
- minimizes second-rewrite risk

Cons:

- does not deliver full orchestrator behavior in one pass
- requires a staged rollout

### 3. Keep the work as reference only and delay implementation

Use the orchestrator work only as design input and postpone all `odin-os` memory work until later phases.

Pros:

- no immediate implementation risk

Cons:

- leaves the canonical runtime behind the legacy repo
- increases drift between the repos

## Recommendation

Choose option 2.

`odin-os` should absorb the workspace memory model as a contract-aligned rewrite. The first pass should establish ownership and storage semantics cleanly. Retrieval sophistication, lifecycle rotation, and richer operator tooling should follow after the foundation is correct.

## Recommended Architecture

Keep `odin-os` as the only canonical runtime owner.

Treat the orchestrator implementation as reference input, not as code to transplant.

Extend the current SQLite and `internal/memory` model so these become first-class memory subjects:

- `workspace`
- `initiative`
- `companion`
- `run`

The authored `memory/` tree remains the canonical source for reviewed durable memory documents where the product chooses authored memory. Mutable runtime truth still belongs in SQLite.

Companions remain first-class runtime roles, but they must use shared scoped memory services rather than companion-private durable stores.

## Scoped Data Model

Keep the existing memory base:

- `conversation_transcripts`
- `memory_summaries`

Extend both tables to support workspace-era ownership:

- `workspace_id`
- `initiative_id`
- `companion_id`

Keep existing lineage:

- `project_id`
- `task_id`
- `run_id`
- `source_transcript_id`

Add first-pass policy fields to `memory_summaries`:

- `visibility_scope`
- `retention_class`

`scope` and `scope_key` may remain temporarily for compatibility and migration, but they should stop being treated as the semantic center of the model.

### Ownership rules

- **Workspace memory:** Marcus-wide preferences, constraints, defaults, and durable context
- **Initiative memory:** knowledge local to one responsibility, including managed-project initiatives
- **Companion memory:** scoped overlay memory for a durable role, not a private truth store
- **Run memory:** transcripts and episodic outcomes tied to execution lineage

### First-pass enum intent

`visibility_scope`:

- `workspace`
- `initiative`
- `companion`
- `run`
- `private_companion_overlay`

`retention_class`:

- `durable`
- `working`
- `episodic`
- `archival`

## Service Shape

Add explicit scoped services:

- `internal/memory/workspaces`
- `internal/memory/initiatives`
- `internal/memory/companions`

Keep the existing services, but reinterpret them:

- `projects` becomes a lineage-aware adapter over initiative-linked memory
- `runs` remains the run-lineage entrypoint
- `knowledge` becomes a generic low-level scoped service rather than the business-facing model
- `users` becomes transitional until workspace identity is authoritative

## Retrieval, Lifecycle, And Operator Behavior

The first `odin-os` pass should stay simple.

### Retrieval

- use structured SQLite queries
- apply explicit scope filters
- keep bounded result counts
- prefer run -> initiative -> workspace -> companion overlay lookup order when a scoped request is assembled

No embeddings or vector retrieval in the first pass.

### Lifecycle

- run transcripts remain persisted operational lineage
- episode summaries stay explicit and lineage-tied
- durable workspace and initiative memory require explicit caller intent
- companion overlay memory defaults to non-durable unless explicitly marked otherwise
- `archival` can exist as a retention class before archive jobs exist

### Operator behavior

First pass should provide read-only visibility, not full memory surgery:

- workspace summary view
- initiative memory view
- companion memory view
- run transcript and episode view

Correction, deletion, promotion, and lifecycle compaction tooling can follow after the scoped model works end-to-end.

## Implementation Sequence

### Phase A: land the owner entities first

Before scoped memory is implemented, `odin-os` needs real persistence and services for:

- `Workspace`
- `Initiative`
- `Companion`

This follows the existing workspace-refactor plan ordering rather than skipping ahead.

### Phase B: extend memory storage and services

After owners exist:

- extend memory rows with owner fields
- add scoped memory services for workspace, initiative, and companion
- adapt project and run memory services to the new semantics

### Phase C: update runtime callers

Update runtime conversation and job flows so new writes carry workspace and initiative ownership directly while preserving run lineage.

### Phase D: add bootstrap and migration helpers

- bootstrap the default Marcus workspace
- create initiatives for existing managed projects
- backfill current global memory into workspace memory
- backfill current project memory into initiative-linked memory
- preserve project, task, and run lineage through migration

### Phase E: add read-only CLI and API views

Expose workspace, initiative, companion, and run memory views through the existing `internal/cli` and `internal/api/http` layers.

## Migration Policy For The Orchestrator Work

The merged orchestrator memory work should stay in place temporarily as a legacy compatibility path while `odin-os` catches up.

The correct treatment under `odin-os` migration policy is `rewrite`, not `migrate_as_is`.

That means:

- copy behavior ideas, not code wholesale
- map semantics onto `odin-os` package contracts
- avoid importing orchestrator-specific `/var/odin` read-model behavior unless explicitly re-justified for `odin-os`

## Non-Goals For The First Odin OS Pass

- no direct port of `/var/odin/memory/hot` projection behavior
- no candidate-memory queue or tombstone system yet
- no full retrieval audit pipeline yet
- no orchestrator-style lifecycle compaction jobs yet
- no companion-private durable silos
- no operator mutation tooling before read/write scope correctness is proven

## Verification Strategy

The first implementation pass should prove correctness in the scoped memory layer before broader features are added.

Minimum verification:

- `go test ./internal/store/sqlite ./internal/memory/... -run 'Test(Memory|Transcript|Episode|Workspace|Initiative|Companion)'`
- `go test ./internal/runtime/...`
- `go test ./internal/cli/... ./internal/api/http`

And at least one integration test proving:

- a default workspace exists
- a managed project becomes an initiative
- scoped memory writes preserve ownership and lineage
- runtime execution still obeys governance after the new memory ownership model lands
