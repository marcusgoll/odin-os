---
title: Initiative And Companion Binding
status: active
date: 2026-04-17
phase: "09"
---

# Initiative And Companion Binding

Companions are runtime roles. Initiatives are durable responsibility streams. Neither is an authored catalog asset.

## Binding model

- A companion may reference catalog capabilities through policy, planning, or curated selection.
- An initiative may bias planning toward specific workflows, skills, or commands through surrounding runtime context.
- Catalog assets remain independently authored as `workflow`, `skill`, `agent_role`, or `operator_command`.

## Rules

- Do not serialize a companion as a workflow or skill just to make it selectable.
- Do not encode initiative identity into catalog keys.
- Use planner input context to bind companions and initiatives to capabilities at runtime.
- Keep provider prompts and executor-specific bundles outside the companion binding model.

## Safe extension path

- Add new companions by changing workspace or companion runtime state.
- Add new authored capabilities by changing registry assets.
- Add new initiative-aware planning behavior by extending planner inputs and selection rules, not by creating duplicate capability kinds.
