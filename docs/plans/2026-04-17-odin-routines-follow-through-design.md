---
title: Odin Routines And Follow-Through Design
status: proposed
date: 2026-04-17
---

# Odin Routines And Follow-Through Design

## Existing State

Odin OS already has the core workspace operating model in place:

- `workspace`, `initiative`, `companion`, `work item`, and `run attempt` are now first-class runtime concepts
- companions already support `assistant`, `advisor`, `operator`, and `specialist`
- initiatives already support `managed_project`, `goal`, `case`, `routine`, `campaign`, and `personal_admin`
- work items already link to workspace, initiative, and companion context
- the REPL and operational HTTP surfaces can already show workspace, initiative, and companion read models
- `odin serve` already runs bounded queue execution and self-heal loops

What is still partial is the user-adaptive life-automation layer above those primitives:

- non-project initiatives are durable state, but not yet first-class operator-managed objects
- companions are durable state, but not yet first-class operator-managed objects outside bootstrap and direct store use
- Odin says it owns schedules and follow-ups, but there is no general follow-up domain yet
- user memory exists, but it is still a thin `global/global` wrapper rather than a workspace-owned Marcus operating profile
- the root command surface does not yet expose direct lifecycle commands for routines, companions, follow-ups, or agenda views

## Product Goal

The next slice should make Odin reliable at recurring life follow-through for Marcus.

In this phase, Odin should:

- support one primary human workspace
- stay bounded proactive rather than broadly autonomous
- turn recurring obligations and reminders into explicit governed work
- keep all external side effects behind existing policy and approval gates

This phase should not build a generic cron platform or fake always-on agent behavior.

## Decisions

### One primary workspace first

This phase optimizes for Marcus as the primary operator of one workspace. The model should stay extensible to household or team support later, but it should not introduce multi-user identity complexity now.

### Bounded proactive automation

Odin may maintain routines, scheduled follow-ups, and reminders. It may create or refresh due work automatically. It may not perform new external side effects directly from the scheduler path without policy checks and normal work-item execution.

### Routines and follow-through before richer advisor behavior

The first value slice should be dependable routine execution and follow-through. Better assistant and advisor behavior can build on top of that foundation later.

## Domain Model

### Workspace

Workspace remains the semantic root for Marcus. It owns:

- operating profile defaults
- policy defaults
- initiative inventory
- companion inventory
- agenda and follow-up visibility

### Initiative

Initiative remains the durable responsibility stream. In this phase it becomes operational for non-project work:

- `routine`
- `personal_admin`
- `goal`
- `case`
- `campaign`

A managed software project remains one initiative kind, not the entire product model.

### Companion

Companion remains the durable role contract. It should represent persistent runtime roles such as:

- `Primary Assistant`
- `Life Admin Operator`
- `Finance Advisor`
- `Research Specialist`

Companions are still not provider-specific prompt bundles and still not continuously running pseudo-persons.

### Operating Profile

Add one new workspace-owned control object: `OperatingProfile`.

`OperatingProfile` holds Marcus-specific defaults that routines and companions need:

- communication preferences
- quiet hours
- approval posture defaults
- follow-up cadence defaults
- privacy boundaries
- escalation defaults

`OperatingProfile` is not a persona system. It is durable operator preference and boundary state.

### Follow-Up Obligation

Add one new durable object: `FollowUpObligation`.

`FollowUpObligation` represents a promised next action, reminder, recurring check-in, or recurring obligation owned by Odin.

It should:

- belong to exactly one workspace
- usually link to one initiative
- optionally link to one owning companion
- support one-time and recurring cadence
- materialize a governed `work item` when due
- preserve history without rewriting the original obligation

This split is important:

- `FollowUpObligation` is the durable promise
- `WorkItem` is the executable instance

Example:

- “Review physical mail weekly” is one obligation
- each weekly cycle becomes one work item

### Work Item and Run Attempt

No new execution object is needed. Existing work-item and run-attempt semantics remain correct:

- a due obligation becomes a work item
- the work item enters the normal planning, approval, routing, and execution path
- one or more run attempts may execute that work item

## Bounded Context Impact

### Workspace

Owns:

- active workspace resolution
- workspace policy defaults
- operating profile

Must not own:

- cadence evaluation
- execution routing
- external side effects

### Initiative

Owns:

- routine and personal-admin initiative lifecycle
- initiative status and ownership assignment

Must not own:

- follow-up scheduling rules
- execution mechanics

### Companion

Owns:

- durable assistant/advisor/operator/specialist role contracts
- initiative ownership relationships
- policy defaults that downstream systems consume

Must not own:

- scheduler execution
- provider prompt construction

### Follow-Through

Add a new supporting context: `follow-through`.

It owns:

- follow-up obligations
- cadence rules
- due evaluation
- obligation status
- work-item materialization rules

It must not own:

- direct tool or provider execution
- project governance
- planning policy

### Work Execution

Work execution remains the only path that actually carries out work. It consumes due obligations only after they become governed work items.

### Memory

Memory should preserve:

- operating profile preferences
- obligation completion history
- missed or blocked follow-up history
- initiative-specific recurring context
- companion-specific operating notes

## Operator Surface

The first operator surface should be explicit root commands, not REPL-only flows.

Recommended commands:

- `odin initiative create --kind routine --key life-admin --title "Life Admin"`
- `odin initiative list`
- `odin companion create --kind advisor --key finance --title "Finance Advisor"`
- `odin companion list`
- `odin profile show`
- `odin profile set --quiet-hours 22:00-07:00`
- `odin followup add --initiative life-admin --title "Review mail" --cadence weekly`
- `odin followup list --due today`
- `odin followup complete <id>`
- `odin followup snooze <id> --until 2026-04-20T09:00:00Z`
- `odin agenda`

The REPL may later wrap these commands, but the root command surface should become authoritative for lifecycle management.

## Runtime Behavior

Inside `odin serve`, Odin should add one bounded follow-through loop:

1. load due follow-up obligations
2. evaluate whether a work item already exists for the current due window
3. materialize a work item if needed
4. record events and projections
5. stop there

The follow-through loop must not:

- call Gmail, Calendar, GitHub, shell, or providers directly
- bypass approval gates
- create a second execution spine

The correct chain is:

- due obligation
- materialized work item
- normal policy checks
- normal queue execution or approval wait

## Failure Model

### Missed cadence

If an obligation is overdue, mark it overdue and surface it clearly in agenda and read models. Do not silently skip it.

### Duplicate materialization

Prevent duplicate work-item creation by obligation plus due-window idempotency.

### Approval required

If policy requires approval, Odin should still create the governed work item, but leave it blocked or pending approval rather than silently executing.

### Missing or inactive companion

If the owning companion is missing or inactive, Odin may fall back to the workspace default companion only when policy allows it. Otherwise it should mark the obligation blocked.

### Archived initiative

If the linked initiative is archived or inactive, Odin should pause linked obligations automatically.

### Executor unavailable

Execution failure does not invalidate the obligation model. The obligation remains owned by Odin, and the resulting work item continues through normal queue and retry behavior.

## Rollout Order

1. add explicit initiative lifecycle commands for non-project initiatives
2. add explicit companion lifecycle commands
3. add the workspace operating profile
4. add the follow-up obligation domain and persistence
5. add agenda and follow-up operator surfaces
6. add bounded serve-loop materialization
7. refine memory and projections for routine history and overdue visibility

This order keeps human control and observability ahead of autonomy.

## Non-Goals

This phase does not build:

- a generic cron platform
- multi-user identity and household support
- broad autonomous side effects without policy approval
- duplicate planner logic just for routines
- fake always-conscious companions

## Result

This design extends the approved Odin workspace operating model into a practical life-automation slice for Marcus.

Odin remains the durable control plane. Companions remain role contracts. Initiatives remain the durable unit of responsibility. Follow-up obligations become the missing durable bridge between responsibility and reliable recurring execution.
