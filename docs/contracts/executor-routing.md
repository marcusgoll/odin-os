---
title: Executor Routing Contract
status: active
date: 2026-04-09
phase: "06"
---

# Executor Routing Contract

`config/executors.yaml` is the authored authority for executor inventory and route policy.

## Config responsibilities

The routing config defines:

- executor inventory
- adapter key
- executor class
- enabled state
- priority
- model reference
- route preference order
- route fallback order

## Route model

Each route matches by portable task properties:

- `task_kinds`
- `scopes`

When a route matches:

1. preferred executors are considered in order
2. unavailable or incompatible executors are skipped
3. configured fallbacks are considered in order
4. the first healthy capability-compatible executor wins

## Selection rules

- disabled executors are ignored
- executors whose class does not match their adapter metadata are rejected
- capability matching uses the portable `TaskSpec`
- broker routes are explicit and not implied automatically

## Notes

- `config/models.yaml` stores model metadata referenced by executors
- routing remains declarative where possible
- provider-specific hardcoding in the selector is not allowed
- provider adapters must stay thin and translate native calls into the canonical capability gateway envelope
- provider-specific prompt shaping belongs at the provider edge, not in manifests or the capability gateway
- MCP surfaces should expose capabilities as typed tools backed by the canonical capability descriptors
