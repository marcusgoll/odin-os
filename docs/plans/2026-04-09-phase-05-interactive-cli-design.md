---
title: Phase 05 Interactive CLI Design
status: accepted
date: 2026-04-09
phase: "05"
---

# Phase 05 Interactive CLI Design

## Goal

Make `odin` the operator-facing front door by introducing a chat-first interactive shell with explicit scope, mode, health, and approval visibility, while keeping Ask mode local and provider-agnostic.

## Chosen Approach

Phase 05 will add a REPL shell with a command bus, a local Ask-mode intent router, and a light session cache.

This keeps the interactive loop low-friction, preserves provider neutrality, and gives later API work a reusable command and rendering layer instead of a CLI-only code path.

## Rejected Alternatives

### Slash-command-only shell

Rejected because it would technically satisfy command coverage but would not feel like talking directly to Odin. Prompt 05 explicitly wants a chat-first operator surface.

### Executor-backed Ask mode

Rejected because Prompt 05 should not bind the CLI to one model provider and because local Ask mode is enough for operational introspection at this phase.

### Full TUI shell first

Rejected because it adds layout and event-loop complexity before the runtime-facing shell contracts are stable.

## Shell Model

`odin` will start an interactive shell by default.

The shell owns:

- a persisted session state with preferred mode and last selected project
- the current resolved scope
- a local Ask/Act mode switch
- slash-command dispatch
- prompt header rendering

The shell does not own executor routing or provider selection in this phase.

## Mode Model

Supported modes:

- `ask`
- `act`

### Ask mode

- Handles free text locally.
- Does not create tasks or runs.
- Answers operational questions from current shell state, manifests, projections, and recent events.
- Falls back to a clear “local ask is limited” response when the input is outside the supported operational question set.

### Act mode

- Keeps the same shell interaction loop.
- Creates structured runtime tasks from non-slash input.
- Requires a non-global scope.
- Must downgrade to `ask` whenever the restored or requested mode is unsafe for the current scope.

## Scope Model

Phase 04 already defines canonical scopes:

- `global`
- `odin-core`
- `project`
- `new-project`

Phase 05 extends that model into the shell:

- the header must always show the current scope
- `/project <key>` selects a managed project scope
- `/self` switches directly to the `odin-core` system project
- `/scope` renders the current scope and why it resolved that way

Global scope remains valid for Ask mode, but Act mode cannot run there.

## Session Persistence

Session persistence will remain intentionally light and live in `state/cache/cli-session.json`.

Persist only:

- last selected project key
- last preferred mode

Startup behavior:

1. read the last session cache
2. validate the saved project against the current manifest registry
3. resolve scope from the validated project
4. validate the saved mode against that scope
5. restore when valid
6. otherwise downgrade safely to either `project + ask` or `global + ask`

No active task, run, approval, or transient prompt history is persisted in this phase.

## Command Surface

Required slash commands:

- `/help`
- `/mode`
- `/scope`
- `/project`
- `/jobs`
- `/runs`
- `/approvals`
- `/logs`
- `/doctor`
- `/self`

### Command behavior

- `/help` lists commands and brief usage.
- `/mode [ask|act]` reads or changes mode.
- `/scope` shows the current resolved scope and project binding.
- `/project [key]` reads or changes the current project selection.
- `/jobs` lists task projections for the current scope.
- `/runs` lists run projections for the current scope.
- `/approvals` lists pending approvals for the current scope.
- `/logs` lists recent audit events for the current scope.
- `/doctor` shows shell/runtime readiness checks.
- `/self` selects `odin-core`.

In this phase, the operator-facing term “job” maps to the existing runtime task primitive.

## Header Rendering

The shell prints a compact header before each prompt.

The header includes:

- scope
- mode
- health summary
- pending approvals count
- current active task or run when present

Health is intentionally conservative in this phase:

- `ok` when the database is reachable and manifests are valid
- `degraded` when one of those checks fails but the shell can continue
- `unknown` when no executor health data exists yet

## Data Access

The shell should not query SQLite ad hoc from unrelated packages.

Read-only services should sit behind narrow helpers:

- manifest registry access from `internal/core/projects`
- scope resolution from `internal/cli/scope`
- task, run, and approval listings from `internal/runtime/projections`
- event-backed log reads from the store event API
- lightweight health checks from a small runtime health service

Act mode task creation can use the existing SQLite task creation path directly through a focused shell service until broader orchestration exists.

## Testing Strategy

Testing should focus on behavior:

- session restore and safe downgrade tests
- header rendering tests
- slash-command handler tests
- Ask mode tests proving free text does not create tasks
- Act mode tests proving free text creates tasks only in allowed scopes
- scope visibility tests across mode and project changes

The primary test seam should be a shell service with injected input lines, output writers, temp SQLite stores, and temporary manifest/session files.

## Phase Boundary

Phase 05 introduces the interactive shell, local Ask mode, scope-aware header rendering, light session persistence, and runtime-backed shell surfaces.

It does not yet introduce:

- provider-backed free-form chat
- background job orchestration beyond task creation
- web transport reuse
- worktree execution
- approval resolution workflows
