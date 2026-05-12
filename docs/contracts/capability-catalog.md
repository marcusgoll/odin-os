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
- catalog inventory must not be treated as runtime implementation proof
- authored registry prompts count as authored assets until a real Odin invocation path, durable output/state, policy enforcement, and audit evidence exist

## Sources in Phase 07

- built-in tool definitions authored in code
- registry-backed `skill` items
- registry-backed `workflow` items
- registry-backed `agent` items mapped to `agent_role`
- registry-backed `command` items mapped to `operator_command`

## Capability truth

`odin overview --json` exposes `capability_catalog` as authored inventory and
`capability_truth` as the conservative implementation readback.

Rules:

- `authored_inventory` mirrors the catalog counts for agent definitions, skills, workflows, commands, and tools
- `authored_asset_count` is inventory size, not implemented automation size
- `runtime_proven_count` may include only capabilities with evidence for invocation, durable output or state where relevant, policy enforcement, and Odin-readable audit evidence
- `partial_count` covers discoverable or invokable items that do not yet satisfy the full proof bar
- `advisory_count` covers authored assets and high-risk surfaces that remain read-only, approval-required, unsupported, or otherwise not runtime-proven
- `items[].truth_level` records the evidence level; `items[].risk_label` records high-risk posture separately so risk posture does not overwrite truth level
