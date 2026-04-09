---
title: Phase 08 Context Compaction Design
status: accepted
date: 2026-04-09
phase: "08"
---

# Phase 08 Context Compaction Design

## Goal

Introduce structured context compaction, durable checkpoints, and append-only wake packets so long-lived Odin work can pause, hand off, switch executors, and resume without carrying raw chat history forward.

## Chosen Approach

Phase 08 will extend the existing SQLite-backed `context_packets` runtime lane with:

- append-only context packet rows
- typed packet scopes for `project_context`, `run_context`, and `task_wake_packet`
- explicit compaction triggers
- structured resume loading from the latest wake packet chain

This keeps compaction auditable, inspectable, and portable across restarts and executor changes.

## Rejected Alternatives

### Mutable latest-packet rows

Rejected because overwriting the latest packet would erase the handoff trail and make pause and resume behavior harder to audit.

### Event-only reconstruction with no durable wake packet

Rejected because replaying the entire event stream on every resume would be slower, noisier, and less portable than loading a purpose-built wake artifact.

### Raw transcript carry-forward

Rejected because Prompt 08 explicitly forbids full chat history as the primary continuation format. Structured compaction is the point of the phase.

## Packet Model

Phase 08 introduces three structured packet schemas:

### `project_context`

Captures stable project state that matters across many tasks:

- project identity and scope
- manifest summary
- governance constraints
- relevant open-task summary
- latest project-level facts needed for future work

### `run_context`

Captures run-local execution state:

- run identity, executor, and attempt
- recent structured tool outputs
- current approvals or blockers
- compacted evidence gathered during the run
- latest status facts needed for retry or resume

### `task_wake_packet`

Serves as the primary portable handoff artifact:

- current objective
- current status and trigger
- blocking reason or pause reason
- latest completed step
- next step list
- known constraints
- selected capabilities
- compacted evidence
- references to linked `project_context` and `run_context` packet IDs

The wake packet should be sufficient to resume work without replaying raw transcripts.

## Persistence Model

`context_packets` remains the canonical durable store for compaction artifacts, but Phase 08 adds queryable envelope fields:

- `packet_scope`
- `trigger`
- `checkpoint_key`
- `supersedes_packet_id`
- `status`

The JSON payload remains the structured packet body. Every packet write stays append-only and emits an auditable event.

## Trigger Model

Phase 08 supports these compaction triggers:

- `handoff`
- `model_switch`
- `approval_wait`
- `token_threshold`
- `idle_pause`
- `completion`
- `restart`

Each trigger writes a new packet rather than mutating a previous one. The latest active wake packet for a task or run is the resume source.

## Resume Model

Resume should load structured state from the latest `task_wake_packet`, then follow referenced `project_context` and `run_context` packet IDs as needed.

The resume loader should return a normalized state object that includes:

- task identity
- active scope
- current objective
- next actions
- current blockers
- compacted evidence
- linked project and run context

This resume state should be portable into executor resume calls and future worker handoff paths.

## Storage and Auditing

Phase 08 should add store helpers for:

- appending typed context packets
- listing packets for a task or run
- loading the latest active wake packet
- loading a packet by ID

Every packet creation should emit a typed runtime event with trigger and packet-scope metadata so compaction history is inspectable from the audit log.

## Integration Boundary

Phase 08 should add a compaction service under `internal/runtime/checkpoints` rather than spreading packet assembly across the store package.

The store remains responsible for durable writes and reads. The checkpoint service is responsible for:

- assembling structured packet payloads
- choosing summaries
- linking project, run, and task packet references
- building normalized resume state from stored packets

## Testing Strategy

Tests should prove:

- packet writes are append-only
- latest wake packet lookup is deterministic
- approval wait and restart compactions produce inspectable wake packets
- resume can rebuild next-step state from wake packets alone
- packet creation emits matching audit events

## Phase Boundary

Phase 08 introduces:

- structured packet schemas
- trigger-driven append-only compaction
- durable wake-packet resume state
- auditable packet creation and resume loading

Phase 08 does not yet introduce:

- transcript compaction for every operator surface
- automatic cross-run packet pruning
- distributed checkpoint replication
- provider-specific resume payload shaping beyond the shared runtime state
