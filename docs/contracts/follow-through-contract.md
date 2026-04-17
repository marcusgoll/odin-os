---
title: Follow-Through Contract
status: active
date: 2026-04-17
---

# Follow-Through Contract

This document freezes the bounded follow-through model for Odin OS. It defines the workspace-owned objects that turn routine intent into durable control-plane state, governed work items, and visible agenda state.

## Scope

Follow-through is the control-plane layer that keeps recurring obligations, reminders, and routine commitments visible and actionable inside a workspace.

It is not a generic scheduler, a background cron system, or a direct execution path. Any real side effect flows through the normal work-item and run-attempt contracts.

## OperatingProfile

`OperatingProfile` is the workspace-owned control object for Marcus-specific operating defaults.

It holds durable preferences that downstream workspace, initiative, companion, and follow-through behavior consume, including:

- communication preferences
- quiet hours
- approval posture defaults
- follow-up cadence defaults
- privacy boundaries
- escalation defaults

Contract rules:

- one workspace owns one active operating profile
- the operating profile is durable control-plane state, not a persona
- the profile informs automation, but it must not directly execute external side effects
- profile changes are explicit workspace mutations and should be visible through workspace projections

## FollowUpObligation

`FollowUpObligation` is the durable promise layer for a next action, reminder, recurring check-in, or other bounded commitment that Odin owns on behalf of the workspace.

It belongs to exactly one workspace and usually links to one initiative. It may also link to an owning companion when a durable role is responsible for the follow-through.

Contract rules:

- an obligation preserves the promise even when the execution instance changes
- an obligation can represent one-time, scheduled, or recurring follow-through
- an obligation carries due information, cadence rules, status, and history
- an obligation does not become a work item until it is due or otherwise materialized by the follow-through loop

Lifecycle states include active, paused, due, blocked, completed, and skipped. Implementations may store these differently, but they must preserve visibility into the same control-plane facts.

## Obligation To Work Item Materialization

When a follow-up obligation becomes due, Odin materializes a governed `work item` from it.

The materialization contract is:

1. identify the workspace and obligation occurrence key for the obligation
2. derive the occurrence key from the obligation identity plus its current scheduled due occurrence, using the normalized due timestamp for one-time obligations and the normalized cadence occurrence for recurring obligations
3. check whether a work item already exists for that exact occurrence key
4. reuse the existing work item when one exists for that occurrence key
5. otherwise create a new work item that references the obligation and occurrence key
6. route the work item through the normal planning, approval, and execution path
7. preserve the original obligation history instead of rewriting the obligation into execution state

This split matters:

- `FollowUpObligation` is the durable promise
- `Work Item` is the executable instance
- `Run Attempt` is one bounded execution pass against that work item

Materialization is idempotent per obligation occurrence key. The same obligation occurrence must reuse the same work item; a new scheduled occurrence must create a new work item.

## Bounded Proactive Behavior

Odin may act proactively inside this contract, but only within a bounded control-plane loop.

Allowed proactive behavior:

- evaluate due obligations
- surface agenda visibility
- materialize work items for due obligations
- mark obligations overdue, blocked, or completed as the state model requires
- record projections and history for later operator review

Disallowed proactive behavior:

- direct calls to Gmail, Calendar, GitHub, shell, or model providers from the scheduler path
- bypassing approval or policy gates
- creating a second execution spine outside the work-item model
- hiding due obligations from the agenda or read models

The proactive boundary is: due obligation -> materialized work item -> normal governed execution.

## Command Surface Rules

The intended root command families for the follow-through model are:

- `odin initiative`
- `odin companion`
- `odin profile`
- `odin followup`
- `odin agenda`

Rules:

- these command families are explicit root entry points
- machine-readable output is available where the command is operational
- the command families surface workspace, initiative, and due-obligation state without implying hidden background execution
- no command family claims to own durable truth outside the workspace model
- the REPL remains a compatibility surface, not the authoritative operator boundary
