---
title: Phase 09 Git Worktrees Design
status: accepted
date: 2026-04-09
phase: "09"
---

# Phase 09 Git Worktrees Design

## Goal

Introduce a task-owned Git worktree, branch, and lease model so multiple mutating Odin tasks can operate safely in parallel without sharing mutable directories or branches.

## Chosen Approach

Phase 09 will add:

- deterministic task-owned branch naming
- global-root worktree path resolution outside managed repo roots
- SQLite-backed worktree leases with heartbeat and cleanup
- explicit read-only execution that skips mutable worktrees

This keeps mutable execution isolated, auditable, and consistent with Odin's existing runtime authority model.

## Rejected Alternatives

### Filesystem-only lease files

Rejected because lease state would become a second mutable authority outside SQLite and would be harder to inspect across projects.

### Git-native worktrees without an Odin lease layer

Rejected because Git does not model task ownership, heartbeat expiry, or lease conflict rules strongly enough for multi-agent orchestration.

### Repo-local worktree roots

Rejected for this phase because the operator explicitly prefers a global root and mutable worktrees should remain outside canonical repo roots.

## Branch Model

Branches belong to tasks, not agents.

The canonical mutable branch format should be:

`odin/<project-key>/task-<task-id>/run-<run-id>/try-<n>`

This gives stable ownership across agent handoffs while allowing retry attempts to create distinct branches without encoding transient agent identity.

Read-only tasks do not allocate a task branch.

## Worktree Path Model

For immediate implementation, Odin should default to:

`~/.config/superpowers/worktrees/odin-os/<project-key>/task-<task-id>/run-<run-id>/try-<n>`

The long-term documented defaults should be:

- local development: `~/.local/share/superpowers/worktrees/odin-os/`
- homelab and server runtime: `/var/odin/worktrees/odin-os/`

All mutable worktrees must live outside the managed repo root.

## Lease Model

Leases are durable runtime records stored in SQLite. A lease binds:

- project
- task
- run
- branch name
- worktree path
- lease mode
- ownership state
- heartbeat timestamp
- release and cleanup state

Each mutable task owns exactly one mutable branch and one mutable worktree lease at a time.

Read-only tasks bypass mutable lease allocation entirely.

## Lifecycle

The Phase 09 manager should support:

- create mutable lease
- attach to an existing active lease for the same task and run
- heartbeat lease
- release lease
- cleanup released or expired leases

Cleanup must be deterministic and must never prune an active lease.

## Package Boundaries

Phase 09 should use:

- `internal/vcs/git` for raw Git commands
- `internal/vcs/branches` for branch naming and retry sequencing
- `internal/vcs/worktrees` for worktree root resolution and git worktree lifecycle
- `internal/vcs/leases` for lease acquisition, heartbeat, release, and cleanup

Lease storage should be implemented through SQLite rather than ad hoc local files.

## SQLite Authority

Phase 09 should add a `worktree_leases` table to the runtime store rather than introducing a separate lease database or filesystem lock format.

The table should be sufficient to inspect:

- current mutable owner
- branch and worktree identity
- active versus released state
- last heartbeat
- cleanup timestamps

## Read-only Behavior

Read-only tasks should run directly against the canonical repo root without creating:

- mutable branch
- mutable worktree
- mutable lease

This phase should make that distinction explicit rather than forcing all tasks through a worktree path.

## Testing Strategy

Tests should prove:

- branch naming is deterministic and retry-safe
- path resolution uses the configured global root
- mutable lease conflicts are denied
- released leases can be cleaned up deterministically
- active leases are not cleaned up
- read-only tasks skip mutable worktree allocation

## Phase Boundary

Phase 09 introduces:

- task-owned branch naming
- global-root mutable worktree paths
- SQLite-backed lease tracking
- deterministic cleanup rules

Phase 09 does not yet introduce:

- remote branch promotion or merge automation
- distributed lease coordination outside a single SQLite authority
- automatic branch deletion policies beyond released-worktree cleanup
