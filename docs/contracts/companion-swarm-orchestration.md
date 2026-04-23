---
title: Companion Swarm Orchestration
status: active
date: 2026-04-17
---

# Companion Swarm Orchestration

This contract defines how Odin OS may use bounded specialist labor behind durable companions without introducing a second orchestration stack.

## Core rule

Companions remain durable control-plane roles. A swarm is an ephemeral supervised execution pattern attached to one parent work item, not a new durable product object.

## Authority model

- Odin owns workspace, initiative, companion, policy, memory, task, run, approval, and projection state.
- Workers own only bounded effort and outputs.
- Swarm coordination must compile down to Odin-owned tasks, runs, approvals, and delegation records.
- There must not be a second policy engine, second runtime authority, or provider-specific swarm path.

## Runtime substrate

The runtime swarm substrate is:

- parent task and optional parent run
- child `delegations`
- child tasks and child runs when work is materialized
- `delegation_artifacts` for structured child outputs

`delegations` are the durable child-assignment contract. They are not a parallel work queue.

## Admission rules

A swarm may be admitted only when all are true:

- the parent objective has at least two bounded subproblems or an explicit verifier step
- the expected outputs can be reconciled through a declared convergence mode
- the parent task still remains the only completion authority for the overall objective
- child count, retry budget, and deadline are explicit

A swarm must be denied when any are true:

- the work can be completed by one normal run attempt
- decomposition would only increase throughput without improving outcome quality
- the parent task is already a child delegation
- stop conditions or aggregation rules are missing

Default depth is one.

## Child bounds

Child delegations may only narrow the parent's effective authority:

- narrower tool access
- narrower mutation mode
- narrower memory visibility
- narrower side-effect authority

Child delegations may never expand beyond workspace, initiative, companion, project-governance, or approval limits already in force for the parent.

## Memory views

Child work consumes narrowed views over existing memory scopes:

- workspace memory
- initiative memory
- companion memory
- parent run memory

Children may propose memory updates, but they do not own private durable memory stores and may not bypass Odin's normal memory write paths.

## Convergence modes

The initial supported convergence modes are:

- `merge`
- `review_gate`
- `rank`
- `quorum`

Open-ended child aggregation logic is out of scope for this contract.

## Result envelope

Each child should emit at least one structured artifact with:

- child status
- summary
- evidence references
- confidence
- unresolved risks
- proposed next actions
- proposed memory candidates

The parent task reconciles those artifacts through the declared convergence mode.

## Operator visibility

Swarm state must remain visible through existing Odin surfaces rather than a second operator console:

- `odin status --json`
- `odin agenda --json`
- `odin companion ...` read surfaces
- existing runtime projections and approvals

No background child work is allowed without a durable task, delegation, approval, or projection record.
