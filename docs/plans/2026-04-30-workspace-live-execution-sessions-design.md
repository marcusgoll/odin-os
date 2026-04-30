---
title: Workspace Live Execution Sessions Design
status: approved
date: 2026-04-30
---

# Workspace Live Execution Sessions Design

## Domain Source of Truth

- `CONTEXT.md`
- `docs/adr/0001-canonical-authority.md`
- `docs/contracts/git-worktrees.md`
- `docs/plans/2026-04-29-delivery-workflow-design.md`

## Current State

Odin already has the runtime substrate needed to represent governed work:

- SQLite runtime authority for projects, tasks, runs, events, worktree leases, context packets, and projection freshness.
- Existing task rows that map to **Work Items** and run rows that map to **Run Attempts**.
- Worktree leases for mutating project work, including task/run-owned branches and isolated paths.
- A deterministic `codex_headless` executor lane and shared executor contract.
- Top-level command dispatch in `internal/app/lifecycle/run.go`.

The current binary does not implement `odin workspace ...` or `odin work ...`; real smoke checks return `unknown command: workspace` and `unknown command: work`.

## Locked Domain Decisions

The v1 workspace surface must preserve these locked rules:

- A **Live Execution Session** is an ephemeral process or attachment handle for one **Run Attempt**.
- A **Live Execution Session Key** is generated from the owning **Run Attempt** and remains the persisted identity.
- Aliases are optional lookup aids, unique among active sessions, and must not replace canonical keys in evidence.
- **Adopted Live Execution Sessions** are explicitly operator-bound external sessions, such as SSH-launched Codex/tmux sessions.
- Odin must not auto-discover or auto-adopt arbitrary SSH, tmux, Codex, or shell sessions.
- Pre-adoption work is handoff context, not Delivery Gate proof.
- One active **Live Execution Session** is allowed per **Run Attempt**.
- Parallel live sessions under one **Work Item** are sibling **Run Attempts**.
- Mutating live sessions require distinct worktree paths and must not share branch, path, or active lease.
- List and overview surfaces use cached liveness with freshness metadata.
- `workspace status` and `workspace attach` are the local live tmux probe or attachment points.
- `workspace attach` is bind-only and must not create, adopt, resume, start, stop, or advance proof.
- `workspace stop` marks Odin lifecycle state by default and must not kill a process without explicit `--terminate`.
- Mutating sessions require `workspace handoff` before stop or terminate unless explicitly abandoned.

## Design Direction

Use an adoption-first `odin workspace ...` command family over existing Odin runtime state. Do not build Odin-launched Codex session creation in v1.

This keeps the first slice focused on the actual operating problem: Codex sessions already started through SSH/tmux on the homelab server need to become visible, attachable, and safely handed off inside Odin without turning tmux into durable authority.

## Command Surface

### `workspace adopt`

Default path:

```text
odin workspace adopt --run <run-id> --host <host> --session-id <tmux-session> --executor codex --mode read_only|mutating [--alias <name>] [--worktree <path>] [--handoff <text>]
```

Behavior:

- Requires an existing **Run Attempt** by default.
- Creates one **Adopted Live Execution Session** bound to that run.
- Generates the canonical session key from the run identity.
- Records host, executor lane, mutation mode, provider session id, repo/worktree path, adoption time, alias, and handoff context.
- Validates alias uniqueness among active sessions.
- For mutating mode, requires a worktree path and validates that active sessions do not share worktree path, branch, or active worktree lease.

Bridge path while `odin work ...` is missing:

```text
odin workspace adopt --new-run --project <key> --title <title> ...
```

`--new-run` explicitly creates the minimal task/run substrate before binding the session. This is a bridge, not the normal path.

### `workspace list`

```text
odin workspace list [--json]
```

Shows cached state only:

- session key and alias
- run, task, and project identity
- executor, host, and provider session id
- mutation mode
- repo/worktree path
- lifecycle state
- cached liveness and freshness timestamp
- handoff status

`list` must not live-probe tmux or SSH.

### `workspace status`

```text
odin workspace status <session-key-or-alias> [--json]
```

Behavior:

- Resolves aliases to canonical session keys.
- For local tmux sessions, probes tmux and updates cached liveness plus projection freshness.
- For remote hosts, reports cached state with `remote_probe_unsupported` in v1.
- If the local tmux session is missing, records cached liveness as stale or missing but does not mark the session stopped automatically.

### `workspace attach`

```text
odin workspace attach <session-key-or-alias>
```

Behavior:

- Bind-only: attaches operator presence to an existing session.
- If local and running in an interactive TTY, attaches with `tmux attach -t <session-id>`.
- If not running in a TTY, prints the exact tmux command and session metadata.
- Does not create, adopt, resume, stop, terminate, or advance proof.

### `workspace handoff`

```text
odin workspace handoff <session-key-or-alias> [key=value...] [--json stdin]
```

For mutating sessions, handoff is required before stop or terminate unless the session is explicitly abandoned.

Required handoff content:

- summary
- changed paths or worktree reference
- last known status
- verification already run
- next recommended action

### `workspace stop`

```text
odin workspace stop <session-key-or-alias> [--abandoned --reason <text>] [--terminate]
```

Behavior:

- Default stop marks the session stopped in Odin only.
- `--terminate` is explicit destructive process intent.
- Mutating sessions require prior handoff unless `--abandoned --reason <text>` is supplied.
- Termination must preserve or request handoff evidence before Odin treats the **Run Attempt** outcome as complete.

## Persistence Model

Add a dedicated `live_execution_sessions` table for current session state.

Required columns:

- `id`
- `session_key`
- `alias`
- `task_id`
- `run_id`
- `project_id`
- `executor`
- `host`
- `session_kind`
- `provider_session_id`
- `mutation_mode`
- `repo_root`
- `worktree_path`
- `branch_name`
- `lease_id`
- `lifecycle_state`
- `cached_liveness`
- `last_probe_at`
- `last_handoff_at`
- `metadata_json`
- `created_at`
- `updated_at`

The table owns current session identity and lifecycle state. Runtime events own the audit history.

Relevant event types:

- `live_execution_session.adopted`
- `live_execution_session.status_refreshed`
- `live_execution_session.attach_attempted`
- `live_execution_session.handoff_recorded`
- `live_execution_session.stopped`
- `live_execution_session.terminate_requested`
- `live_execution_session.abandoned`

Projection freshness should track list/overview liveness freshness, not become canonical session state.

## Service Design

Add `internal/runtime/workspace` as the session lifecycle service.

Primary responsibilities:

- Generate stable session keys from run identity.
- Resolve aliases to canonical session keys.
- Validate one active session per run.
- Validate mutating-session worktree isolation.
- Create adopted sessions.
- Support explicit `--new-run` bridge creation through existing task/run substrate.
- Record lifecycle events.
- Update cached liveness and projection freshness.
- Enforce handoff-before-stop for mutating sessions.

The service should use small injected adapters for local tmux probing and attachment so tests can fake process behavior without requiring real tmux.

## Error Behavior

- Unknown key or alias: print not found and suggest `workspace list`.
- Alias collision: reject and show colliding active session keys.
- Mutating adopt without worktree: reject.
- Mutating adopt with shared active worktree, branch, or lease: reject.
- Remote status: do not SSH; report cached state and `remote_probe_unsupported`.
- Non-TTY attach: print command instead of hanging.
- Local tmux missing during status: mark cached liveness stale or missing, not stopped.
- Stop without required handoff: reject and list required handoff fields.
- Terminate without supported local process handle: report unsupported and leave session state unchanged.
- `--new-run` failure: fail before creating a session row.

## Testing and Proof

Focused tests:

- Command parsing for adopt/list/status/attach/handoff/stop.
- Store tests for `live_execution_sessions`, session key uniqueness, alias uniqueness, and active-session uniqueness by run.
- Service tests for adoption, alias resolution, local tmux probing, cached liveness updates, stop behavior, handoff requirements, abandon exception, and worktree isolation.
- Integration tests proving real command dispatch through `odin workspace ...`.

Real Odin proof after implementation:

```text
go build -o ./bin/odin ./cmd/odin
ODIN_ROOT=<temp> ./bin/odin doctor
ODIN_ROOT=<temp> ./bin/odin workspace adopt --new-run --project odin-core --title "Adopt local Codex tmux" --host local --session-id <fixture> --executor codex --mode read_only --alias codex-test
ODIN_ROOT=<temp> ./bin/odin workspace list
ODIN_ROOT=<temp> ./bin/odin workspace status codex-test
ODIN_ROOT=<temp> ./bin/odin workspace attach codex-test
ODIN_ROOT=<temp> ./bin/odin workspace handoff codex-test summary="..." changed_paths="none" last_status="done" verification="doctor" next_action="stop"
ODIN_ROOT=<temp> ./bin/odin workspace stop codex-test
```

The attach proof should include a non-TTY check that prints the tmux command rather than hanging.

## Deferred

- `workspace start` for Odin-launched Codex/tmux sessions.
- Remote SSH liveness probing.
- Remote process termination.
- Overview/TUI rendering beyond cached session list data.
- Full integration with future `odin work ...` gate advancement.

## Open Implementation Risks

- The active checkout is dirty; implementation should happen in a clean isolated worktree or carefully scoped branch.
- `odin work ...` is absent, so the `--new-run` bridge must stay explicit and temporary.
- The current `codex_headless` lane is deterministic alpha behavior, not real Codex CLI process launch.
- Local tmux availability must be treated as an environment precondition for live probing/attachment tests.
