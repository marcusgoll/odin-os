---
title: Capability Budget Contract
status: active
date: 2026-04-09
phase: "07"
---

# Capability Budget Contract

The tool broker enforces separate tool and context budgets.

## Tool budget

Tool budget controls:

- maximum selections
- maximum invocations
- maximum cumulative cost units

Selections and invocations both consume budget cost units.

## Context budget

Context budget controls:

- maximum expanded definitions
- maximum compacted results
- maximum compacted payload bytes

## Denial behavior

Budget denials must be explicit and structured.

The broker must reject over-budget operations rather than silently truncating or attaching oversized capability definitions.
