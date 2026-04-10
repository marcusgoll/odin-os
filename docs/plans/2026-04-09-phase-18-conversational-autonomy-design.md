---
title: Phase 18 Conversational Autonomy Design
status: accepted
date: 2026-04-09
phase: "18"
---

# Phase 18 Conversational Autonomy Design

## Goal

Make Odin feel like talking to Claude or Codex while preserving Odin OS runtime discipline: real conversational answers in `ask`, durable execution in `act`, scoped persistent memory, autonomous learning, and autonomous self-editing with rollback.

## Current Problems

The current runtime is honest about Phase 05, but it does not yet match the desired operator experience:

- `ask` mode is still a small local intent router and falls back to a placeholder for generic prompts
- `act` mode creates durable tasks, but prompt submission does not attempt immediate execution or explain later policy failures inline
- memory exists only as authored repo structure; there is no scoped runtime memory service for prompts, outcomes, and learned patterns
- learning proposals and promotions exist, but they assume explicit approval and runtime-only activation
- the system cannot yet autonomously generate, canary, promote, self-edit, and merge its own improvements

## Approaches Considered

### 1. Minimal CLI upgrade

Make `ask` executor-backed and leave long-term memory and autonomous self-improvement for later.

This would improve the shell quickly, but it would still feel like a thin REPL over disconnected subsystems.

### 2. Runtime-agent model

Use the shell as the front door to one runtime agent with two intents:

- `ask` for synchronous conversational work with read-only tools and memory
- `act` for durable runtime tasks with immediate foreground execution and background continuation

Persist memory by scope, derive learning proposals from repeated interactions, auto-promote through canaries, and self-edit canonical assets through Odin-owned branches and worktrees.

This is the recommended approach.

### 3. Fully unified always-mutate agent

Remove most distinctions between `ask` and `act` and let every prompt plan, mutate, learn, and self-edit immediately.

This is closest to raw autonomy, but it weakens operator clarity, makes intent ambiguous, and increases blast radius when learning goes wrong.

## Recommendation

Adopt the runtime-agent model.

### Interaction model

`ask` should become a real synchronous conversation lane. A free-form prompt should:

- resolve scope
- load scoped memory
- load a read-only tool context
- route through the live executor
- return a real answer directly in the shell

`act` should remain the durable mutation lane. A free-form prompt should:

- create a tracked task and run record
- perform one immediate foreground execution attempt through the same runtime path used by `odin serve`
- print success, failure, or policy denial inline
- continue in the background under `odin serve` when longer-running behavior is needed

This keeps the experience conversational while preserving explicit runtime truth.

### Memory model

Odin should maintain three memory layers:

1. global personal memory
2. per-project memory
3. runtime-derived episodic memory

Global memory stores stable preferences, habits, and cross-project patterns. Project memory stores local architecture, conventions, failures, and project-specific skills or tools. Episodic memory stores prompt/response traces, tool results, task outcomes, and execution evidence before compaction.

Reads should merge these layers by scope:

- project scope reads project memory first, then global memory
- global scope reads only global and explicitly shareable memory
- promotion of memory upward should require repeated evidence across scopes

Writes should land in the narrowest valid scope first.

### Learning model

Every `ask` and `act` interaction should be eligible as learning input.

Odin should mine:

- repeated prompts
- repeated tool chains
- repeated recovery sequences
- repeated operator corrections
- repeated project-specific work patterns

into candidate:

- prompt refinements
- routing changes
- retry changes
- playbooks
- skills
- tools
- memory summaries

The existing proposal lifecycle should remain the auditable substrate, but approval should become autonomous rather than human-gated for this personal-assistant deployment.

### Promotion and canary model

Auto-promotion should not jump directly to global default behavior.

The activation path should be:

1. derive proposal from recorded evidence
2. evaluate proposal with replay or sandbox fixtures
3. auto-promote into a bounded canary lane
4. observe live canary outcomes
5. either promote to default or auto-rollback

Canaries should be bounded by scope, proposal type, target key, and volume, for example:

- one project only
- one tool only
- ten interactions only

Failed canaries must rollback automatically and emit explicit audit events.

### Self-edit model

Odin should be allowed to rewrite canonical files on its own, including:

- `config/*.yaml`
- `registry/`
- `prompts/`
- `memory/`

But self-editing must not write directly to the default branch or the live working tree.

Instead, Odin should:

1. create an Odin-owned task
2. allocate an Odin-owned worktree and task branch
3. apply the proposed edits there
4. run replay, tests, health checks, and any policy checks
5. auto-merge if the canary and verification pass
6. auto-rollback if the verification or canary fails

This preserves autonomy while keeping rollback and auditability intact.

### Tool model

`ask` mode should allow autonomous read-only tool use.

`act` mode should allow mutating tool use and file edits subject to scope, branch/worktree isolation, and verification. The shell should no longer force the operator to switch modes just to perform read-only inspection during a conversational answer.

### Guardrails

For this personal-assistant deployment, autonomy should be maximized and human approval minimized. The guardrails should therefore be containment and reversibility, not operator confirmation.

The required guardrails are:

- scope-partitioned memory
- evented audit history for interactions, learning, canaries, self-edits, and rollbacks
- Odin-owned branches and worktrees for self-editing
- mandatory verification before merge
- automatic rollback on canary failure
- no direct writes to the default branch

### Operator visibility

Even with high autonomy, the operator should be able to inspect:

- scoped memory summaries
- latest ask and act transcripts
- active canaries
- active learned promotions
- rollback history
- self-edit branches and merge results

This visibility belongs in the shell and runtime projections, not hidden package globals.

## Runtime Shape

Phase 18 should add a thin conversation service that composes:

- scope resolution
- scoped memory
- tool broker access
- executor selection
- transcript persistence

The shell should call that service synchronously for `ask`.

The existing job service should be extended to support:

- foreground execution of a newly created task from the shell
- the same execution path under `odin serve`
- inline reporting of policy failures instead of delayed discovery through logs

Learning, canary control, and self-edit orchestration should remain separate services under `internal/learning`, backed by SQLite and the existing VCS isolation mechanisms.

## Testing Strategy

Testing should prove the runtime truth, not just isolated helpers:

- shell tests for generic `ask` prompts producing real executor-backed answers
- shell tests for `act` prompts executing immediately and surfacing denials inline
- store and memory tests for scoped transcript and summary persistence
- integration tests for `ask` with read-only tool use and memory recall
- learning tests for automatic proposal creation, canary activation, promotion, and rollback
- self-edit tests for branch/worktree isolation, verification, and auto-merge behavior
- replay tests proving deterministic evaluation for autonomous promotions

## Non-Goals

Phase 18 does not need to solve:

- multi-user memory sharing
- organization-wide approval workflows
- direct default-branch mutation
- autonomous infrastructure operations outside the local Odin runtime
- opaque hidden learning paths outside SQLite-backed runtime authority
