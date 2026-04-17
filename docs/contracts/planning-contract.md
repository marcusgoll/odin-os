---
title: Planning Contract
status: active
date: 2026-04-17
phase: "09"
---

# Planning Contract

Planning consumes bounded runtime context and a thin capability catalog. It does not own authored assets, provider selection, or execution state.

## Required planning inputs

- `scope`
- `workspace`
- `initiative`
- `companion`
- `memory_references`
- `catalog selections`

## Input rules

- `workspace` is always present and identifies the durable operating environment.
- `initiative` is optional but should be supplied when work is tied to a managed project, routine, case, or other responsibility stream.
- `companion` is optional but should be supplied when the plan must respect a durable assistant or advisor role.
- `memory_references` are summaries or handles, not raw transcript dumps.
- Planning may filter or rank capabilities using this context, but the context itself is not an authored capability.

## Planner outputs

- thin cards suitable for inspection before expansion
- expanded capability definitions for selected items only
- compacted tool results when tool invocation is explicitly requested

## Boundary rules

- Planning may compose `workflow`, `skill`, `agent_role`, `operator_command`, and built-in `tool` capabilities.
- Planning must not collapse companions into authored capability cards.
- Planning must not store mutable runtime truth in planner-local memory.
- Planning must not bypass project governance or execution routing.
