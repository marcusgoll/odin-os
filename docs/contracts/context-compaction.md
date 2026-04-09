# Context Compaction Contract

## Purpose

Phase 08 defines the durable compaction artifacts Odin uses to pause, hand off, and resume long-lived work without carrying raw full chat history forward.

## Canonical Store

Compaction artifacts are stored in SQLite in `context_packets`. Packet rows are append-only. A newer packet may supersede an older packet, but older packets remain available for audit and replay.

## Packet Scopes

`context_packets.packet_scope` identifies the structured packet body:

- `project_context`
- `run_context`
- `task_wake_packet`

## Triggers

`context_packets.trigger` identifies why compaction happened:

- `handoff`
- `model_switch`
- `approval_wait`
- `token_threshold`
- `idle_pause`
- `completion`
- `restart`

## Packet Status

`context_packets.status` indicates packet lifecycle:

- `active`: current resume candidate
- `superseded`: replaced by a newer packet
- `sealed`: final packet for a completed or intentionally closed state

## Payload Schemas

### `project_context`

- `project_id`
- `project_key`
- `scope`
- `manifest_summary`
- `policy_summary`
- `open_task_summary`
- `facts`

### `run_context`

- `run_id`
- `task_id`
- `executor`
- `attempt`
- `status`
- `approval_summary`
- `tool_results`
- `facts`

### `task_wake_packet`

- `task_id`
- `task_key`
- `scope`
- `objective`
- `status`
- `trigger`
- `blocking_reason`
- `last_completed_step`
- `next_steps`
- `constraints`
- `selected_capabilities`
- `evidence`
- `project_context_packet_id`
- `run_context_packet_id`

## Resume Rule

Resume loads the latest active `task_wake_packet` for the task, then loads the linked `project_context` and `run_context` rows by packet ID when those links are present.

## Audit Rule

Every packet write must emit an auditable runtime event. The event payload must include:

- `packet_kind`
- `packet_scope`
- `trigger`
- `status`
- `summary`

## Non-Goals

This contract does not require:

- raw transcript persistence as a resume artifact
- distributed checkpoint replication
- automatic packet garbage collection
