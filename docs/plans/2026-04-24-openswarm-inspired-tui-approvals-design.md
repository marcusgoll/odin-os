# OpenSwarm-Inspired TUI Approvals Design

**Date:** 2026-04-24

**Status:** Approved design; implementation started in `codex/openswarm-tui-approvals-main`

**Source comparison:** https://github.com/openswarm-ai/openswarm, reviewed 2026-04-24

## Goal

Adopt the useful operator-experience lessons from OpenSwarm without changing Odin's runtime authority model. V1 should improve Odin's real operator surface through a canonical `/overview` board plus a stronger approvals lane, while leaving execution state, approvals, work items, runs, and workflow continuations owned by existing Odin runtime services.

## Existing State Found

- `CONTEXT.md` defines Odin OS as workspace-governed orchestration for durable execution, memory, approvals, and delegated work.
- `docs/contracts/tui-overview.md` already locks the default operator hierarchy as `Workspace -> Initiative -> Work Item -> Run Attempts` plus side lanes for Approvals, Observability, Memory, Intake Inbox, Companions, and Capability Catalog.
- `docs/plans/2026-04-23-odin-tui-overview-implementation.md` already proposes a read-only `/overview` command that composes a canonical TUI board from existing runtime truth.
- `./bin/odin --help` does not currently list `/overview`.
- The real shell has `/approvals`, but the current handler is pending-list only.
- SQLite runtime state already has `approvals`, `RequestApproval`, `ResolveApproval`, pending approval projections, and approval lifecycle events.

## OpenSwarm Features Considered

The OpenSwarm README presents a local multi-agent workbench with:

- spatial dashboard cards for agents, views, and embedded browser cards
- unified human-in-the-loop approvals across agents
- keyboard shortcuts for navigation and approval decisions
- worktree isolation and diff inspection
- streaming chat, branches, persistent sessions, tools, skills, modes, templates, and local-first storage

Odin should not copy OpenSwarm's agent-dashboard authority. Odin already has a stronger governed runtime model. The useful transfer is the operator ergonomics: a single place to see active work, approvals, linked evidence, and safe next actions.

## Locked Decisions

- V1 target surface is `TUI/operator overview first`.
- The OpenSwarm-inspired v1 includes `/overview + approvals`.
- Approval mutation is `scoped resolve`, not generic resolve.
- `/approvals resolve` must refuse to mutate any pending approval that has no registered safe resolver or continuation contract.
- Unsupported approvals remain inspectable in `/overview` and `/approvals`.
- Approval support filters are list/inspection filters only; they never imply batch approve or deny authority.

## Design

Build a TUI-first operator workbench slice over existing Odin state:

### `/overview`

`/overview` is a read-only canonical board. It should expose the existing TUI contract hierarchy:

1. `Workspace`
2. `Initiative`
3. `Work Item`
4. nested `Run Attempts`

It should also show side lanes for:

- `Approvals`
- `Observability`
- `Memory`
- `Intake Inbox`
- `Companions`
- `Capability Catalog`
- `Automation Triggers`

The board must translate storage-era terms into canonical operator language without renaming tables or creating new persistence.

### Approval Lane

The approval lane in `/overview` should show:

- pending approval count
- newest pending approval handles
- linked work item key
- linked run id when present
- resolver support state, such as `supported` or `unsupported`
- compact next command hints for supported approvals

It must not become the primary landing surface. Approvals remain a governance side lane mirrored inside `Work Item` detail.

### `/approvals`

The base `/approvals` command should become a richer pending list. Suggested shell receipt format:

```text
approval=<id> task=<work-item-key> run=<id|none> status=pending resolver=<supported|unsupported>
```

This remains a list surface, not a transcript or evidence dump.

Support filters may narrow the list to resolver-backed or unsupported pending approvals:

```text
/approvals supported
/approvals unsupported
```

These filters are intentionally batch-safe: they change visibility only. They must not add batch approve or deny behavior, and they must not make `supported` approvals mutable without the normal explicit `/approvals resolve <id> approve|deny because <reason...>` path.

### `/approvals show <id>`

`/approvals show <id>` should expose detail needed for human review:

- `approval=<id>`
- governance status
- linked work item
- linked run attempt
- requested timestamp
- decision timestamp when resolved
- decision actor when resolved
- reason when resolved
- resolver support status
- evidence pointers such as `/runs show <run-id>`

It should point to existing evidence surfaces instead of duplicating run artifacts inline.

### `/approvals resolve <id> approve|deny because <reason...>`

Resolution is a scoped operator action. The CLI parses intent, but runtime workflow-owned resolver logic owns mutation and continuation.

Rules:

- If the approval is unsupported, print a refusal and leave state unchanged.
- If the approval is not pending, refuse mutation and leave state unchanged.
- If approved and the resolver starts continuation, print the returned continuation run handle.
- If denied, do not print a fake `run=`.
- Output stays compact and receipt-like.

Approve receipt shape:

```text
approval=<id> status=resolved result=approved run=<submit-run-id>
summary=approval granted; continuation started
```

Deny receipt shape:

```text
approval=<id> status=resolved result=denied
summary=approval denied; later retry requires fresh prepare when applicable
```

Unsupported receipt shape:

```text
approval=<id> status=unsupported result=not_resolved
summary=approval has no registered resolver; inspect only
```

## Runtime Boundary

The CLI and TUI own presentation and command routing only.

Workflow-owned approval resolvers own:

- whether an approval is resolvable
- allowed approve and deny transitions
- calls to storage-level `ResolveApproval`
- wake-packet sealing or supersession
- continuation run creation
- denial consequences on the owning work item
- returned handles for operator follow-up

The storage-level `ResolveApproval` function remains an implementation primitive. It is not the operator authorization boundary.

## Invariants

- `Approval Request` status remains one of `pending`, `approved`, `denied`, or `expired`.
- `Work Item`, `Run Attempt`, and `Approval Request` statuses stay separate.
- `/overview` is read-only.
- `/approvals resolve` never calls storage-level `ResolveApproval` directly for unsupported approvals.
- Unsupported approvals are visible but immutable through the shell resolve path.
- Support filters do not mutate approval state and do not authorize batch action.
- Approval resolution must be idempotence-safe: a non-pending approval cannot be resolved again.
- Approval resolution must preserve workflow-specific continuation semantics.
- Denial must not create a continuation run handle.
- Approval detail must point to existing evidence surfaces rather than duplicating artifacts inline.
- Social Copilot approval-ready drafts continue to use `/memory resolve`; they must not be silently migrated into `/approvals`.

## Rejected Options

### Copy OpenSwarm's Agent Dashboard

Rejected because Odin's center is workspace-governed orchestration, not a free-form agent-card dashboard. A dashboard that treats agents as the primary object would conflict with `Workspace -> Initiative -> Work Item`.

### Generic Approval Resolve

Rejected because it would let the shell mutate governance state without proving workflow continuation, denial handling, or wake-packet semantics.

### Approval-First Landing Page

Rejected for v1 because approvals are a governance lane, not Odin's primary business navigation.

## Implementation Handoff

Context: `internal/cli` operator surface over the existing runtime, approval, projection, memory, registry, and shell services.

Owns: `/overview` presentation, approval lane presentation, `/approvals` list/detail/resolve routing, resolver-support display, read-only approval support filters.

Does not own: approval storage authority, workflow continuation semantics, transfer-specific submit behavior, social draft approval, browser-control evidence, or physical table renames.

Canonical terms: `Workspace`, `Initiative`, `Work Item`, `Run Attempt`, `Approval Request`, `Operator Surface`, `Capability Catalog`, `Automation Trigger`.

Avoid terms: generic `process` lane, top-level `agent dashboard`, generic `approval queue` as the primary landing object, `approval_id` in shell receipts, batch approval actions.

Boundary crossings: CLI routes to runtime approval resolver contracts; runtime resolvers call storage and workflow services; overview reads projections and registry snapshots.

Open blockers: none for implementation planning.
