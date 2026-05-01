# Git Worktrees Contract

## Purpose

Phase 09 defines how Odin allocates task-owned branches, mutable worktrees, and durable leases for managed Git projects.

## Branch Naming

Mutable task branches use this format:

`odin/<project-key>/task-<task-id>/run-<run-id>/try-<n>`

Rules:

- branch names belong to tasks, not agents
- project keys are sanitized to lowercase dash-separated segments
- retry attempts increment `try-<n>`

## Worktree Roots

Immediate implementation default:

- `~/.config/superpowers/worktrees/odin-os/`

Long-term documented defaults:

- local development: `~/.local/share/superpowers/worktrees/odin-os/`
- homelab and server runtime: `/var/odin/worktrees/odin-os/`

## Worktree Paths

Mutable worktrees use this format:

`<root>/<project-key>/task-<task-id>/run-<run-id>/try-<n>`

Mutable worktrees must live outside the canonical repo root.

## Read-only Tasks

Read-only tasks do not allocate:

- mutable branch
- mutable worktree
- mutable lease

## Operator Projection

`odin workspace list` is a read-only projection over active worktree leases. It must not allocate a branch, create or remove a worktree, mutate lease state, adopt a live process, attach to tmux/SSH/Codex, or imply that Live Execution Session lifecycle controls are implemented.
