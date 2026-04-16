---
title: Odin Operating Model Contract
status: active
date: 2026-04-16
---

# Odin Operating Model Contract

This contract freezes the stable product ownership model for Odin OS. New product-facing work should treat this document as the semantic center unless a later ADR or contract explicitly supersedes it.

## Product thesis

Odin OS is Marcus's persistent workspace operating system. Odin is the control plane that owns durable state, policy-aware dispatch, and follow-through across software, research, life admin, and ongoing operations.

Odin is not a wrapper around Codex, Claude, or any single executor. Model providers and worker runtimes are replaceable execution infrastructure inside an Odin-owned system.

## Control plane vs execution plane

### Control plane

Odin owns the persistent workspace and its durable decisions:

- workspace state
- initiatives
- companions
- policy
- memory
- work items
- schedules and follow-ups
- approvals
- queues
- checkpoints and projections
- audit history
- dispatch decisions

If a worker crashes or a provider changes, Odin still knows what the work was, what happened, and what must happen next.

### Execution plane

Workers are short-lived execution-plane components. They receive bounded assignments, resolved context, allowed tools, budgets, and stop conditions through Odin's shared contracts. Workers may inspect code, call tools, run tests, draft artifacts, or perform approved side effects.

Workers do not own durable truth. They do not define policy. They do not become the system of record. They return effort, outputs, evidence, and follow-up recommendations back to Odin.

The stable ownership rule is: Odin owns state and decisions; workers own bounded effort and outputs.

## Durable product objects

### Workspace

The top-level durable environment for Marcus. A workspace owns default preferences, integration boundaries, schedules, policy defaults, and the inventory of active initiatives and companions. Odin should stay friendly to one primary workspace without inventing heavy multi-tenant complexity.

### Initiative

A durable unit of responsibility. Initiatives are the main container for meaningful work. A managed software project is one initiative kind, not the whole product model.

### Companion

A durable role contract such as Daily Assistant, Project Operator, Finance Advisor, or Research Analyst. A companion defines charter, allowed tools, memory rules, planning defaults, tone, and escalation posture. A companion is not a fake autonomous persona.

### Policy

The explicit rule layer that governs what Odin and its workers may do. Policy covers approvals, budgets, tool access, external side effects, project mutation, privacy, retention, and escalation. Background capability without policy is not allowed.

### Memory

Durable, typed records with provenance. Memory is not an unbounded transcript dump. Odin should keep memory across workspace, initiative, companion, run, and user-preference contexts with source, owner context, and validation or confidence metadata.

### Work Item

The durable operational object that turns intent into governed execution. A work item belongs to a workspace and usually links to an initiative. It may also link to a companion, a managed project, approvals, or scheduled follow-up.

### Run Attempt

One execution attempt against a work item. Run attempts are disposable execution records that capture what happened in the execution plane. They do not replace the durable work item.

## Work flow

1. Marcus captures intent in the workspace directly or through a companion.
2. Odin resolves workspace, initiative, and policy context.
3. Odin creates or updates a work item.
4. Odin decides whether to ask for approval, queue work, or dispatch a worker.
5. A worker executes one bounded run attempt and returns outputs plus evidence.
6. Odin records the result, updates memory and work state if allowed, and schedules the next obligation.

## Design implications

- Managed projects are governed initiative types, not a second product architecture.
- Companions are durable operating roles, not continuously running pseudo-persons.
- Workers and executors remain replaceable as long as they satisfy Odin's execution contracts.
- Background capability must surface as explicit work state, policy, or follow-up owned by Odin.
