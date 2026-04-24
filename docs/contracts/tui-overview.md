---
title: TUI Overview Contract
status: active
date: 2026-04-23
phase: "18"
---

# TUI Overview Contract

## Purpose

Define the canonical Odin TUI overview so future operator-surface work reads one contract instead of inferring structure from shell command families, runtime table names, or storage-era nouns.

## Existing state found

- `CONTEXT.md` now locks the canonical TUI hierarchy, lane semantics, and compatibility-alias rules.
- `docs/contracts/workspace-context-map.md` already defines `Capability Catalog`, `Memory`, `Observability`, `Work Execution`, and related bounded contexts that the TUI should project.
- The real repo-owned `./bin/odin --help` surface still exposes transport-era command families such as `/project`, `/agent`, `/workflow`, `/memory`, `/jobs`, `/runs`, `/approvals`, `/logs`, and `/doctor`.
- The current REPL and runtime packages still use some storage-era words such as `project`, `task`, and `run`, even though the canonical operator language has moved to `Initiative`, `Work Item`, and `Run Attempt`.

## Canonical hierarchy

The default operator path is:

1. `Workspace`
2. `Initiative`
3. `Work Item`
4. nested `Run Attempts`

This hierarchy is the primary business navigation. Future TUI work must not replace it with a run monitor, queue board, or generic process dashboard.

## Dashboard order

`/overview` is a dashboard-first projection over the canonical hierarchy, not a second operator model.

Rules:

- render `Attention` first for approvals, incidents, blocked work, recoveries, and other items that need operator intervention
- render `Active Execution` second for live `Run Attempts` and active companion-swarm execution so operators can see who is working on what
- keep the canonical lanes below those sections so dashboard triage does not replace `Workspace -> Initiative -> Work Item`
- do not introduce a separate generic `Agents` or `Processes` dashboard to answer runtime triage questions already covered by `Companions`, `Run Attempts`, `Approvals`, and `Observability`

## Primary lanes

### Workspace

The landing surface starts here. It should summarize:

- workspace identity
- current control scope
- initiative inventory
- top-level policy and readiness cues
- entry points to scoped controls such as automation triggers

### Initiative

This is the main narrowing step under the workspace. It should show:

- initiative status and summary
- scoped work
- assigned companions
- scoped memory snippets
- initiative-scoped automation triggers

### Work Items

This is the primary governed-work lane.

Rules:

- `Work Item` is the main managed object for durable obligations.
- `Work Queue` is a filtered execution-state view inside this lane, not a separate top-level lane.
- default work detail is business-first rather than repo-first or execution-first.

### Intake Inbox

This is a first-class lane for raw arrivals before governed work exists.

Rules:

- raw intake lives here as `Intake Items`
- intake may be suppressed, answered directly, enriched, re-triaged, or linked to work
- intake must not be collapsed into the work queue or work-item lane
- current `task_intakes` rows are task-linked intake evidence, not full raw `Intake Item` authority
- `/overview` may surface task-linked intake evidence only if it labels it as linked or triaged intake; a fully live `Intake Inbox` lane requires Workspace-first raw intake persistence and projection

### Companions

This lane is for durable AI operating roles only.

Rules:

- show `Companions` here
- do not treat registry `agent` entries as companions by default
- do not treat child execution or delegations as companion state

### Capability Catalog

This is the first-class authored-definition lane.

It contains typed sections or filters for:

- workflows
- skills
- tools
- agent definitions
- operator commands when surfaced as catalog items

It must not be split back into multiple top-level panes just because the shell currently exposes separate commands.

### Approvals

This is the first-class governance triage lane.

Rules:

- show cross-scope pending and recent `Approval Requests`
- keep linked approvals visible inside `Work Item` detail too
- do not let this become the primary business landing surface

### Observability

This is the first-class runtime-understanding lane.

It contains:

- logs
- health
- metrics
- incidents
- recoveries
- projections
- runtime readbacks that cut across initiatives or work items

Rules:

- `Run Attempts` remain nested under `Work Item` detail in the default business view
- `Run Attempts` may also be browsed here for cross-scope debugging and operator understanding
- observability consumes runtime truth and must not become a second authority

### Memory

This is the first-class scoped-knowledge lane.

Rules:

- memory views must always use explicit `Memory Scope`
- workspace, initiative, companion, and run-related memory may all appear here
- relevant memory snippets should also appear contextually inside other detail views
- do not turn this into an unscoped notes dump

## Scoped controls

### Automation Triggers

The user-facing idea of "processes" maps to `Automation Triggers`, not to a new first-class process object.

Rules:

- `Automation Triggers` belong to one `Workspace` and may narrow to one owning `Initiative`
- surface them under workspace or initiative controls
- do not add a top-level `Processes` lane
- triggers create or update `Work Items` before any worker dispatch
- v1 schedule-backed triggers are `Follow-Up Obligations`; they should appear as trigger definitions with derived due or overdue state
- materialized follow-up occurrences remain `Work Items` with follow-up provenance and should not be duplicated as trigger definitions

### Nested runtime and governance surfaces

- `Run Attempts` are nested inside `Work Item` detail and mirrored in `Observability`
- `Approval Requests` are nested inside `Work Item` detail and mirrored in `Approvals`
- `Memory` is contextual inside detail views and browseable in the `Memory` lane

## Compatibility aliases

The current shell remains a compatibility surface. The TUI should translate these nouns into canonical language rather than mirror them one-for-one.

- `project`: compatibility alias for initiative selection or managed-project transport language
- `agent`: compatibility name for `Agent Definitions` or worker/sub-agent execution, not a synonym for `Companion`
- `job` or `task`: storage-era or shell-era language for `Work Item`
- `run`: shell shorthand for `Run Attempt`
- `process`: avoid; use `Automation Trigger`, `Work Item`, `Run Attempt`, or `Observability` depending on meaning

## Default operator flow

1. Start on `Workspace`.
2. Narrow to one `Initiative`.
3. Inspect or act on a `Work Item`.
4. Review nested `Run Attempts`, linked `Approval Requests`, and contextual memory.
5. Jump sideways to `Approvals`, `Observability`, `Memory`, `Intake Inbox`, or `Capability Catalog` only when the task calls for that cross-cutting view.

## V1 operator entrypoint

The first canonical interactive-shell entrypoint for this board should be `/overview`.

Rules:

- keep `odin workspace ...` reserved for project Codex workspace lifecycle
- keep `/workspace`, `/initiatives`, and `/companions` as adjacent read-only transport surfaces until `/overview` fully supersedes their operator value
- route ask-mode overview questions to the same `/overview` surface instead of a second dashboard path

## Non-goals

- a run-monitor-first landing page
- a top-level `Processes` lane
- a separate top-level `Work Queue` dashboard parallel to `Work Items`
- an unscoped memory board
- collapsing raw intake into governed work
- treating current shell command families as the final TUI taxonomy
