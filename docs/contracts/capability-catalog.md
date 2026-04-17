---
title: Capability Catalog Contract
status: active
date: 2026-04-09
phase: "07"
---

# Capability Catalog Contract

The tool broker exposes a thin capability catalog before any full definitions are loaded.

## Capability kinds

- `tool`
- `skill`
- `workflow`
- `agent_role`
- `operator_command`

## Thin card fields

- `kind`
- `key`
- `title`
- `summary`
- `scopes`
- `tags`
- `applies_to`
- `composes`
- `cost_hint`
- `budget_cost`
- `source_ref`

## Rules

- thin cards must not include full tool schemas
- thin cards must not include full skill bodies
- thin cards must not include full workflow bodies
- thin cards must not include full agent-role definitions
- thin cards must not include full operator-command definitions
- expansion occurs only after selection

## Sources in Phase 07

- built-in tool definitions authored in code
- registry-backed `skill` items
- registry-backed `workflow` items
- registry-backed `agent` items mapped to `agent_role`
- registry-backed `command` items mapped to `operator_command`
