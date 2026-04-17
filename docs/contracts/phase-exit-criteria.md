---
title: Phase Exit Criteria
status: active
date: 2026-04-08
phase: "00"
---

# Phase Exit Criteria

This document defines the acceptance gate for Phase 00 and the minimum gate every later phase must satisfy before being considered complete.

## Universal exit criteria for every phase

Every phase must satisfy all of the following:

1. Scope is explicit.
The phase states whether it changes global behavior, `odin-core`, managed-project behavior, or new-project setup behavior.

2. Authority is preserved.
The phase does not introduce a second canonical runtime authority or blur authored files with mutable runtime state.

3. Package ownership remains clear.
Changed folders and packages still have non-overlapping responsibilities consistent with `docs/contracts/repo-layout.md`.

4. Governance remains enforceable.
If the phase introduces mutating project work, it routes through Git-aware project governance, isolated worktrees, task-owned branches, and auditable leases.

5. Dynamic loading is respected.
The phase does not preload the entire tool, skill, agent, or executor catalog when scoped loading is sufficient.

6. Determinism and observability improve or hold steady.
The phase preserves deterministic behavior where possible and emits events, projections, logs, and metrics for the behavior it introduces.

7. Self-modification remains bounded.
Any self-heal or self-improvement behavior introduced by the phase is policy-bounded, auditable, replay-tested when applicable, and reversible.

8. Tests and verification exist.
The phase adds or updates the tests, replay coverage, or verification commands needed to prove its behavior.

9. Documentation and migration notes exist.
The phase updates docs for new contracts and records migration notes when it replaces or removes legacy concepts.

10. No accidental permanence is introduced.
Temporary shims, compatibility paths, or stopgaps are either explicitly time-bounded or rejected.

## Phase 00 exit criteria

Phase 00 is complete only when all of the following are true:

- `docs/adr/0001-canonical-authority.md` exists and defines a single canonical runtime authority.
- `docs/adr/0002-migration-policy.md` exists and defines the allowed migration actions.
- `docs/contracts/repo-layout.md` exists and gives non-overlapping responsibilities to top-level folders and internal package groups.
- `docs/contracts/phase-exit-criteria.md` exists and defines both the Phase 00 gate and the universal later-phase gate.
- `README.md` gives a new contributor a concise explanation of the system architecture and current repo status.
- Project governance rules explicitly cover local Git projects, optional GitHub-backed projects, and the reserved `odin-core` system project.

## How later phases should prove exit

Before a later phase is closed, it should be able to answer these questions directly:

- What new authority, contract, or behavior did this phase introduce?
- Which canonical source owns the new data?
- Which packages own the implementation?
- How is scope identified and surfaced?
- How is the behavior tested and observed?
- What legacy concept was replaced, and where was that migration recorded?

If a phase cannot answer those questions cleanly, it is not ready to exit.

## Operational autonomy exit criteria

No phase, service mode, or cutover plan may claim operational readiness or primary controller status unless all of the following are true:

- fresh bootstrap reaches healthy state without manual seeding
- at least one real executor lane completes durable work end to end
- high-risk work is blocked behind explicit approval records
- mutable work uses leased task-owned worktrees and branches
- interrupted work can be recovered after restart
- multi-project queue control exists and prevents starvation or uncontrolled backlog growth
