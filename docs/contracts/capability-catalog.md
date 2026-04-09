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
- `sub_agent`

## Thin card fields

- `kind`
- `key`
- `title`
- `summary`
- `scopes`
- `tags`
- `cost_hint`
- `budget_cost`
- `source_ref`

## Rules

- thin cards must not include full tool schemas
- thin cards must not include full skill bodies
- thin cards must not include full sub-agent definitions
- expansion occurs only after selection

## Sources in Phase 07

- built-in tool definitions authored in code
- registry-backed `skill` items
- registry-backed `agent` items mapped to `sub_agent`
