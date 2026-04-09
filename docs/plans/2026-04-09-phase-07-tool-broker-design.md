---
title: Phase 07 Tool Broker Design
status: accepted
date: 2026-04-09
phase: "07"
---

# Phase 07 Tool Broker Design

## Goal

Introduce a dynamic tool broker that exposes thin capability catalog cards first, expands full tool or skill definitions only when selected, and enforces explicit tool and context budgets.

## Chosen Approach

Phase 07 will build the broker over:

- built-in tool definitions authored in code
- local registry-backed skills and agents
- planner-side integration only

This keeps the catalog deterministic and testable while the broker contract stabilizes.

## Rejected Alternatives

### Full external environment discovery

Rejected because plugin, connector, and runtime environment discovery would add unstable inputs before the broker’s contract and budgets are reliable.

### Preloading complete tool schemas and skill bodies into planning context

Rejected because it violates the phase objective directly. The broker should expose only thin cards first.

### Shell-wide immediate integration

Rejected because Prompt 07 is primarily about planning and execution efficiency. Planner-side integration is enough for this phase without spreading broker concerns across every operator surface.

## Capability Catalog Model

Phase 07 adds a thin catalog format for three capability kinds:

- `tool`
- `skill`
- `sub_agent`

Each card should include only:

- key
- title
- summary
- kind
- scopes
- tags
- cost hint
- budget cost
- source reference

The thin catalog must not include:

- full tool schemas
- full skill bodies
- full agent prompt definitions

## Expansion Model

Capabilities expand only on selection.

Expansion returns the full selected definition:

- tool schema and invocation metadata for tools
- full body and sections for skills
- full body and sections for sub-agents

Expansion is budgeted. If an expansion would exceed context budget, the broker should reject it with a structured denial reason.

## Budget Model

Phase 07 introduces two distinct budgets:

### Tool budget

Controls:

- total selections
- total invocations
- cumulative capability cost units

### Context budget

Controls:

- number of expanded capability definitions
- compacted result count
- maximum compacted payload size

Budget denials must be explicit and structured rather than silently truncating context.

## Structured Results

Tool invocation results should return a structured envelope rather than a raw transcript:

- `summary`
- `artifacts`
- `key_facts`
- `follow_on_options`
- `raw_ref`

The broker should support compaction into a smaller reusable form that preserves planning value while avoiding transcript growth.

## Planner Integration

Phase 07 should integrate the broker with the planning side of task execution rather than full runtime execution.

The planner path should:

1. request a thin capability catalog
2. plan from cards only
3. expand a capability only when chosen by the plan
4. avoid sub-agent expansion unless the selected plan step asks for it

This is the minimum useful integration that satisfies the prompt without smearing broker logic across every runtime path.

## Testing Strategy

Tests should prove:

- thin catalogs omit full definitions
- selected capabilities expand correctly
- tool budgets and context budgets deny excess use
- structured results compact cleanly
- planner integration starts from thin cards and expands only chosen capabilities

## Phase Boundary

Phase 07 introduces:

- thin capability cards
- on-demand capability expansion
- explicit tool and context budgets
- structured compactable result envelopes
- planner-side broker integration

Phase 07 does not yet introduce:

- external plugin or connector discovery
- shell-wide broker usage
- automatic sub-agent spawning
- transcript-level compaction across the whole runtime
