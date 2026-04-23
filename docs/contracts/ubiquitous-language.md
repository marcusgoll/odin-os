---
title: Ubiquitous Language Contract
status: active
date: 2026-04-16
---

# Ubiquitous Language Contract

This document freezes the canonical language for Odin OS. New design, planning, and implementation work should use these terms instead of the older project-centric vocabulary.

## Canonical terms

| Term | Meaning | Notes |
| --- | --- | --- |
| `Workspace` | The top-level operating environment for a human or organization in Odin. It is the semantic root for all durable work, memory, and execution. | Every initiative, companion, and execution lane belongs to exactly one workspace. |
| `Initiative` | A durable responsibility stream inside a workspace. Initiatives can represent work, life, or ongoing operational commitments. | Managed projects are one specialized initiative type. |
| `Companion` | A durable AI role attached to a workspace, optionally scoped further to one or more initiatives. Companions include assistants, advisors, operators, and specialists. | Companion is the user-facing term for a persistent AI role. |
| `OperatingProfile` | The workspace-owned control object that stores durable user-operating defaults such as communication preferences, quiet hours, approval posture, follow-up cadence, privacy boundaries, and escalation defaults. | The operating profile is control-plane state, not a persona. |
| `FollowUpObligation` | The durable promise layer for a next action, reminder, recurring check-in, or other bounded commitment owned by Odin on behalf of a workspace. | Follow-up obligations materialize into work items when due. |
| `Managed Project` | A governed initiative subtype with explicit policy, reviewability, and Git-backed mutation rules. | Use this term when the initiative is project-shaped and subject to project governance. |
| `Work Item` | The durable unit of governed work. A work item is what gets planned, routed, executed, and completed. | This is the preferred unit for product semantics and operator-facing language. |
| `Run Attempt` | One execution attempt for a work item inside an execution lane. | Run attempts are disposable records of execution, not the durable work itself. |
| `Control Scope` | The explicit authority boundary that determines what the operator and system may inspect, mutate, or route. | This is the preferred term for authority boundaries. |
| `Execution Lane` | A bounded execution path that carries work items through planning, runtime execution, and retries. | Lanes are operational capacity and routing constructs, not the work itself. |

## Legacy or narrowed terms

The following terms still appear in implementation and migration surfaces, but they are not the preferred product vocabulary:

- `task` is a narrowed term for executor payloads, runtime jobs, or external system mappings. Use `work item` in product language.
- `agent` is a narrowed term for internal worker constructs, registry payloads, or model-backed roles. Use `companion` in product language.
- `command` is a narrowed term for CLI entrypoints, approved invocation verbs, or adapter-level triggers. Use the specific action or work item name where possible.
- `scope` is a narrowed term for implementation details about control boundaries or execution boundaries. Use `control scope` when the authority boundary matters to the product.

## Canonical relationships

- A workspace contains initiatives.
- A workspace owns the operating profile and follow-up obligations.
- An initiative may contain work items and companions.
- A managed project is a governed initiative, not a separate top-level concept.
- A follow-up obligation may materialize one or more work items over time.
- A work item may produce multiple run attempts over time.
- A run attempt occurs inside exactly one execution lane.
- Control scope determines what can be seen or changed at each step.

## Usage rules

- Use the canonical terms in new docs, planning artifacts, UI copy, and APIs unless the legacy mapping is the point of the document.
- Do not reintroduce `task`, `agent`, `command`, or `scope` as primary product nouns in new contracts.
- When a legacy term is unavoidable, define it once in the same document and map it back to the canonical term.
