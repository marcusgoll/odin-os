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
- registry-backed skills must be discoverable from a fresh registry load, not a stale startup snapshot
- the default built-in catalog must expose only runtime-backed operator tools
- placeholder or canned operational tools must not appear in the default catalog

## Sources in Phase 17

- runtime-backed built-in tool definitions authored in code
- registry-backed `skill` items
- registry-backed `agent` items mapped to `sub_agent`

## Current posture

As of Phase 17, the default built-in operator catalog is intentionally empty until a built-in tool is wired to a real runtime surface. Registry-backed skills and sub-agents remain discoverable from the canonical registry, and executable skills can be invoked through the shared skill service rather than per-skill broker hacks. Placeholder operational tools such as canned project status, task list, or event log summaries are not advertised.
