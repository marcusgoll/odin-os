---
title: Odin OS Operating Model
status: proposed
date: 2026-04-16
---

# Odin OS Operating Model

## Existing State

Odin OS already has several durable runtime foundations:

- SQLite as the canonical runtime authority
- authored registry assets for commands, workflows, skills, and agent roles
- policy, approval, projection, checkpoint, and recovery subsystems
- provider-neutral executor routing and worker dispatch
- managed project governance with worktree isolation

What is still partial is the product model above that runtime. The repo has strong execution primitives, but the user-facing operating model is still split across project governance, executor plumbing, and generic runtime language. The missing piece is one stable definition of what Odin owns persistently versus what short-lived workers do temporarily.

## Product Thesis

Odin OS is Marcus's persistent operating system for getting work done across software, life admin, research, operations, and follow-up.

Odin is not a wrapper around Codex or Claude. Odin is the governed control system around AI labor. It owns durable workspace state, decides what work may happen, remembers what matters, schedules background work, and supervises execution through policy-aware worker dispatch.

The product promise is simple: Marcus interacts with one adaptive workspace, and Odin turns intent into governed execution and reliable follow-through.

## Core Objects

### Workspace

The durable top-level environment for Marcus. Workspace owns default preferences, integration boundaries, schedules, policy defaults, and the inventory of active initiatives and companions. Odin should assume one primary workspace for Marcus without hard-coding a heavyweight multi-tenant model.

### Initiative

A durable unit of responsibility. Initiatives are the main container for meaningful work. A managed software project is one initiative kind, not the entire product model. Other initiative kinds include `goal`, `case`, `routine`, `campaign`, and `personal_admin`.

### Companion

A durable operating role such as Daily Assistant, Project Operator, Finance Advisor, or Research Analyst. A companion is not a fake autonomous being. It is a role contract: charter, assigned initiatives, tool permissions, planning defaults, memory rules, tone, and escalation policy.

### Policy

The explicit rule layer that governs what Odin and its workers may do. Policy covers approvals, budgets, external side effects, tool access, project mutation, privacy, retention, and escalation. Background automation without policy becomes hidden chaos, so policy must be first-class.

### Memory

Durable scoped records with provenance. Memory is not a giant transcript dump. Odin should keep typed memory across workspace, initiative, companion, run, and user-preference scopes. Every memory record should carry source, scope, retention intent, and confidence or validation state.

### Work Item and Run Attempt

`Work Item` is the durable operational object that turns intent into execution. It belongs to a workspace and usually links to an initiative, optionally to a companion and managed project. `Run Attempt` is one execution attempt for that work item. Runs are disposable. Work items are durable.

## Control Plane vs Execution Plane

### Control Plane: what Odin owns

Odin owns persistent workspace state, initiatives, companions, policy, memory, work items, schedules, approvals, checkpoints, queues, projections, audit history, and dispatch decisions. It also owns the capability catalog, integration contracts, and the rules for when project governance or approval gates apply.

This is the actual product. If a worker crashes, Odin still knows what the work was, what happened, what is blocked, and what needs to happen next.

### Execution Plane: what workers own

The execution plane is where short-lived workers run bounded assignments. A worker may inspect code, draft artifacts, call tools, run tests, or perform approved external actions through Codex, Claude, or another executor behind Odin's shared execution contract.

Workers do not own durable truth. They do not define policy. They do not become the system of record. They provide effort and outputs against Odin-owned work.

The clean ownership rule is: Odin owns state and decisions; workers own bounded effort and outputs.

## Human Interaction Model

Marcus interacts with Odin, not directly with raw model runtimes. The main surfaces should be:

- a workspace home showing agenda, blocked items, approvals, and follow-ups
- initiative views showing status, next actions, deadlines, and linked projects
- companion conversations for role-shaped interaction such as assistant, advisor, or operator
- approval and review surfaces for risky actions, external side effects, or completed results

Background capability means scheduled and event-driven automation inside policy bounds. It does not mean pretend always-conscious agents.

## Worker Model

Workers are short-lived execution units created to advance one work item or one work-item step. Workers should be typed by job, not by brand: planner, researcher, builder, reviewer, operator, summarizer.

Codex, Claude, and future executors sit behind Odin's executor contract as replaceable lanes. Odin chooses the lane based on policy, cost, tool access, reliability, and task shape. The user should care about the companion and result, not the provider, unless policy or debugging makes the executor relevant.

Every worker handoff should include:

- objective
- control scope and linked initiative
- assigned companion, if any
- relevant memory packet
- allowed tools and budget
- stop conditions
- expected output schema
- approval requirements

On completion, the worker returns outputs, evidence, proposed memory updates, and recommended follow-ups. Odin then records the run, applies policy, updates durable state, and schedules the next obligation.

## Flow: Idea to Execution to Follow-Up

1. Marcus captures intent in the workspace, directly or through a companion.
2. Odin resolves workspace, initiative, and companion context.
3. Odin creates or updates a work item.
4. Odin plans the next bounded step and checks policy.
5. If allowed, Odin dispatches a worker through the best execution lane.
6. The worker performs the bounded task and returns outputs plus evidence.
7. Odin records the run, updates work state, writes approved memory, and determines whether approval, another step, or follow-up is required.
8. Odin places the next obligation back into the workspace queue so nothing falls through the cracks.

## Top 10 Architectural Principles

1. Odin is the product; model providers are replaceable infrastructure.
2. One persistent control plane, many short-lived execution lanes.
3. Companions are durable role contracts, not running pseudo-persons.
4. Initiatives are the main unit of responsibility; projects are a special case.
5. Policy must gate action before execution, not explain it afterward.
6. Memory must be typed, scoped, and attributable; no global memory soup.
7. Workers are expendable; work items, decisions, and audit history are not.
8. Keep the system a modular monolith until real scale forces otherwise.
9. Every background behavior must surface as explicit work, state, or policy.
10. Prefer simple, inspectable runtime objects over clever autonomous abstractions.

## Resulting Direction

Odin should evolve as a workspace operating system with managed projects as one governed initiative kind, companions as role contracts, policy as the action gate, memory as a typed durable substrate, and workers as temporary execution labor. That gives the product a stable center without turning it into either project-only orchestration or provider-branded AI theater.
