---
title: Delivery Workflow Design
status: approved
date: 2026-04-29
---

# Delivery Workflow Design

## Domain Source of Truth

- `CONTEXT.md`
- `docs/adr/0001-canonical-authority.md`
- `docs/contracts/registry-format.md`
- `docs/contracts/verification-model.md`

## Current State

Odin already has the lower runtime substrate for governed work:

- SQLite runtime authority for projects, tasks, runs, approvals, leases, events, checkpoints, and projections.
- Markdown-with-frontmatter registry entries for workflows, skills, agents, and commands.
- A real shell operator surface with project scope, act mode, jobs, runs, workflows, approvals, transitions, and TradeBoard commands.
- Executor routing through a shared contract, including the deterministic `codex_headless` alpha lane.
- Worktree and task-owned branch enforcement for mutating project work.
- Verification rules that require real `odin` command proof for operator-visible behavior.

The missing product surface is the Delivery Workflow: one repeatable, evidence-backed way for Odin to take a Work Item from intent to verified completion across software, docs, audit, ops, research, and skill-authoring work.

## Locked Domain Decisions

The delivery hierarchy is:

1. **Initiative**: durable unit of responsibility.
2. **Work Item**: durable operational object that turns intent into governed execution.
3. **Run Attempt**: one execution attempt for a Work Item through an executor lane.

Feature work is represented as a **Feature Work Item** or a grouped set of Work Items. `Feature` is not a separate top-level aggregate in v1.

The canonical Delivery Workflow operator surface is top-level `odin work ...`. REPL slash commands may alias this surface, but they are not the proof authority.

The v1 Delivery Workflow gates are:

1. `domain_locked`
2. `design_approved`
3. `plan_ready`
4. `execution_selected`
5. `execution_complete`
6. `verified`
7. `branch_finished`
8. `learning_reviewed`

Delivery Profiles are authored as specialized `workflow` registry entries tagged `delivery_profile`. They are not a fifth registry kind in v1.

## Design Direction

Use a thin `odin work ...` command family over existing runtime services rather than creating a parallel pipeline.

The command family should render canonical language while reusing the current implementation substrate:

- Existing task rows can initially back Work Items.
- Existing run rows can initially back Run Attempts.
- Existing workflow registry entries can declare Delivery Profiles.
- Existing jobs, runs, projections, executor routing, and worktree leases continue to own runtime execution behavior.
- Existing verification model remains the proof contract.

This keeps v1 small enough to ship while aligning the operator surface with the locked domain model.

## Delivery Profiles

V1 Delivery Profiles are workflow registry entries with `delivery_profile` in `tags`.

Each Delivery Profile should declare:

- applicable Work Item kinds or shapes
- required gates
- required and optional skills
- allowed agents or reviewer roles
- proof requirements
- failure branches
- clean output expectations

Starter profiles should include:

- `feature_delivery`
- `bugfix_debugging`
- `audit_only`
- `docs_update`
- `ops_remediation`
- `research_brief`
- `skill_authoring`

Profiles may use lighter proof for lower-risk work, but they must not remove verification or learning review. A profile may satisfy those gates with a short explicit evidence record when that is appropriate.

## Feedback Loop

A clean feedback loop produces:

- current gate
- evidence
- decision
- next action
- remaining risk

Skills, agents, and workflows are selected by the current Work Item kind, risk, scope, and proof needs. They do not own state. Odin owns Work Item state, gate transitions, approval, evidence, verification, and branch-finish decisions.

Unexpected errors, test failures, build failures, and confusing behavior route through `systematic_debugging`.

`writing_skills` is optional after `learning_reviewed`. It runs only when the learning review identifies a reusable process gap that merits skill work.

## `odin work ...` Surface

The v1 command family should start small:

- `odin work profiles`
- `odin work start`
- `odin work status`
- `odin work advance`
- `odin work evidence`
- `odin work verify`

The shell may later provide slash aliases, but every user-visible Delivery Workflow behavior must be provable through the top-level command path.

## Testing And Verification

Implementation must follow the existing verification model:

- Unit tests for profile parsing, gate ordering, and state transitions.
- Registry tests proving tagged workflow profiles load through existing registry mechanics.
- Integration tests proving Work Items and Run Attempts map onto existing task/run substrate without breaking current job execution.
- Real `odin work ...` command checks against a fresh `ODIN_ROOT`.
- Failure-path checks for missing profiles, invalid gate transitions, and attempts to advance without evidence.

Tests should emphasize clean signal:

- clear current gate
- direct missing-evidence messages
- short next-action output
- explicit remaining risks

## Rejected Alternatives

### Add a fifth `profile` registry kind immediately

Rejected for v1 because the current registry supports only agents, skills, workflows, and commands. Workflow entries already compose skills and agents and declare procedures. A future ADR can promote Delivery Profiles to a separate registry kind if workflow entries prove insufficient.

### Schema-first workspace migration

Rejected for v1 because the repo already has task/run execution machinery that can carry the first operator loop. A deeper Work Item schema can follow once the command surface proves useful.

### Registry-only prototype

Rejected because it would document the loop without enforcing gate evidence through Odin-owned runtime state.

## Open Implementation Questions

- Which existing task fields should be rendered as Work Item fields without schema churn?
- Where should gate evidence persist in v1: events, checkpoints, task metadata, or a small new table?
- Which `odin work start` inputs are required for the first useful workflow?
- How should profile recommendation work without silently overriding authored profile requirements?
- What is the minimum branch-finish behavior for non-code Work Items?

## Approval

Approved design direction: thin `odin work ...` over existing runtime, with v1 Delivery Profiles as tagged workflow entries.
