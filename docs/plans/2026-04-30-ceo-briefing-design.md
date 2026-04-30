---
title: CEO Briefing Design
status: approved
date: 2026-04-30
---

# CEO Briefing Design

## Domain Source of Truth

- `CONTEXT.md`
- `docs/adr/0001-canonical-authority.md`
- `docs/adr/0002-migration-policy.md`
- `docs/contracts/registry-format.md`
- `docs/contracts/runtime-events.md`
- `docs/contracts/observability.md`
- `docs/contracts/verification-model.md`

## Current State

The legacy CEO briefing still runs from `/home/orchestrator/odin-orchestrator` and writes runtime artifacts under `/var/odin`. Those artifacts are useful migration and audit evidence, but they are not an Odin OS authority path.

Current Odin OS already has the primitives needed for a canonical replacement:

- SQLite runtime authority.
- Append-only runtime events.
- Approval records and approval projections.
- Workflow registry entries.
- Runtime health, projections, jobs, runs, and logs.
- A real repo-owned `odin` binary that must prove user-visible behavior.

The missing product surface is a canonical operator-invoked CEO briefing path inside Odin OS.

## Locked Domain Decisions

The canonical operator surface is:

- `odin brief ceo`

The CEO briefing behavior is operator-invoked, not scheduled in v1. It is portfolio-wide by default and may support filters such as project key or business date.

The workflow produces a **Briefing Proposal** first. The proposal may include focus weights, blockers, queue recommendations, and slot recommendations, but it must not publish priorities, mutate queue state, or change scheduler behavior.

Approval publishes a **Daily Priority Packet** only. A packet is priority evidence, not a queue action. Queue deferral, cancellation, release, dispatch, or scheduler changes require separate operator actions and evidence.

For one business date, only one approved **Daily Priority Packet** may be active at a time. A later approved packet supersedes the prior active packet through append-only history.

SQLite runtime state and append-only events are the canonical authority. Generated files, emails, Telegram messages, and legacy `/var/odin/state/priorities.json` are artifacts only.

## Recommended Approach

Use a command-first briefing workflow: add a top-level `odin brief ceo` command over existing Odin OS runtime services.

`odin brief ceo` gathers current Odin OS evidence, stores a Briefing Proposal, creates a pending approval, and renders the proposal. Existing approval resolution publishes the Daily Priority Packet. This directly replaces the legacy sidecar authority path with a canonical Odin-owned loop:

1. Generate proposal.
2. Request approval.
3. Resolve approval.
4. Publish packet.
5. Prove through real command output.

## Lifecycle

`odin brief ceo` creates a Briefing Proposal for a business date. The proposal includes evidence references, focus weights, blockers, deferred or killed recommendations, and slot recommendations. It creates a pending approval but does not publish priorities or mutate queue state.

Approval resolution publishes a Daily Priority Packet into SQLite. If another packet is already active for that date, the new packet supersedes it through an append-only event. Rejection leaves the proposal recorded but unpublished.

Lifecycle states:

- `drafted`: proposal generated and stored.
- `approval_requested`: approval row and event created.
- `rejected`: operator rejected the proposal or approval expired.
- `published`: approval resolved positively and packet created.
- `superseded`: prior active packet replaced by a later approved packet for the same business date.

## Data Model

Add SQLite-backed proposal records:

```text
briefing_proposals
- id
- business_date
- scope filter fields
- status
- evidence_json
- proposal_json
- approval_id nullable
- created_at
- updated_at
```

Add SQLite-backed packet records:

```text
daily_priority_packets
- id
- business_date
- status: active or superseded
- proposal_id
- supersedes_packet_id nullable
- packet_json
- approved_at
- created_at
```

Add append-only event types:

- `briefing.proposal_created`
- `briefing.approval_requested`
- `briefing.proposal_rejected`
- `briefing.packet_published`
- `briefing.packet_superseded`

`proposal_json` and `packet_json` carry focus weights, blockers, recommendations, and slot guidance. Queue recommendation IDs are references only and do not imply queue mutation.

## Operator Surface

Canonical command family:

- `odin brief ceo`
  - Generates a portfolio-wide Briefing Proposal for today's UTC business date by default.
  - Creates pending approval.
  - Prints proposal id, approval id, business date, focus summary, recommendation count, and next command.
- `odin brief ceo --date YYYY-MM-DD`
  - Generates for a specified business date.
- `odin brief ceo --project <key>`
  - Filters evidence and recommendations to one project while still producing a dated proposal.
- `odin brief ceo status [--date YYYY-MM-DD]`
  - Shows latest proposal and active packet state.
- `odin brief ceo show <proposal-or-packet-id> [--json]`
  - Shows stored proposal or packet evidence.
- `odin brief ceo --json`
  - Emits machine-readable output for tests and automation.

Approval resolution remains on the existing approval surface. `brief ceo` does not add `--approve` in v1.

## Evidence Sources

V1 `odin brief ceo` reads from current Odin OS only:

- runtime health and doctor summary
- pending approvals
- current tasks, jobs, and runs
- project manifest and project transition state
- workflow registry snapshot and diagnostics
- recent runtime events
- executor health
- projection freshness
- known open incidents or recoveries
- optional legacy audit note when legacy `/var/odin` CEO artifacts are detected, clearly marked `legacy_reference`

The evidence collector emits `evidence_json` with source names, timestamps, status, and gaps. If a source is unavailable, the proposal records the missing source and continues unless the missing source is required to produce a truthful briefing.

V1 does not call email, Telegram, Sabbatic, `/var/odin/engine.db`, or `odin-orchestrator` scripts.

## Approval And Publication

When `odin brief ceo` generates a proposal:

1. Create a `briefing_proposals` row with status `drafted`.
2. Create an approval request linked to the proposal.
3. Update the proposal to `approval_requested`.
4. Append proposal and approval events.
5. Print the operator-facing next step.

When approval is resolved positively:

1. Lock by business date in the same transaction.
2. Find the current active packet for that date, if any.
3. Create a new `daily_priority_packets` row with status `active`.
4. Mark the prior active packet `superseded`, if present.
5. Append `briefing.packet_published` and optional `briefing.packet_superseded`.
6. Mark the proposal `published`.

When approval is rejected or expires:

1. Mark the proposal `rejected`.
2. Append `briefing.proposal_rejected`.
3. Do not create or change packets.

Resolving the same approval more than once must return the existing packet result and must not create another active packet.

## Testing And Verification

Implementation must include:

- Unit tests for proposal lifecycle transitions.
- Unit tests for packet supersession rules.
- Unit tests for idempotent approval resolution.
- Unit tests proving queue recommendation references do not mutate queue state.
- Store tests for proposal and packet persistence.
- Store tests proving only one active packet per business date.
- Store tests proving append-only event emission.
- CLI tests for `odin brief ceo --json`.
- CLI tests for `odin brief ceo status --json`.
- CLI tests for missing or invalid dates and unknown project keys.

Real command proof is required:

1. Use a fresh `ODIN_ROOT`.
2. Run `./bin/odin brief ceo --json`.
3. Resolve approval through the real Odin approval path.
4. Run `./bin/odin brief ceo status --json`.
5. Prove proposal generation alone did not change queue or task statuses.

## Rollout

First implementation slice:

- proposal generation
- approval request
- status and show surfaces
- Daily Priority Packet publication
- supersession
- real command proof

Legacy `/var/odin` CEO automation remains non-authoritative until a separate shutdown or migration task disables it.

Email, Telegram, Sabbatic posting, scheduled briefing, and direct queue mutation are out of scope for v1.

## Rejected Alternatives

### Registry workflow first, command as viewer

Rejected for v1 because it risks producing another documented workflow before the runtime can enforce packet lifecycle.

### Projection-only MVP

Rejected because it would show current state but would not replace the legacy briefing's core value: approved priority publication.

### Inline approval with `odin brief ceo --approve`

Rejected because it blurs generation and publication. Approval resolution should remain the explicit publish boundary.

### Scheduled CEO briefing in v1

Rejected because background scheduling would recreate the legacy sidecar shape before Odin owns the command, approval, and packet lifecycle.

## Open Implementation Questions

- Which existing approval resolver path should invoke packet publication without creating CEO-specific approval shortcuts?
- Should `briefing_proposals` and `daily_priority_packets` use integer ids, stable string keys, or both for operator references?
- What exact JSON envelope should `odin brief ceo --json` return for proposal and packet references?
- Which current runtime sources are required versus optional for a truthful first briefing?
- How should future `odin brief` subcommands share collector and rendering code without over-generalizing v1?

## Approval

Approved design direction: command-first `odin brief ceo` over existing Odin OS runtime services, with explicit approval before publishing a Daily Priority Packet.
