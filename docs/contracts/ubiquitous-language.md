---
title: Ubiquitous Language Contract
status: active
date: 2026-04-16
---

# Ubiquitous Language Contract

This contract defines the canonical product vocabulary for Odin OS and narrows older runtime terms that still exist elsewhere in the repo.

## Canonical terms

Use these terms in new product-facing docs, contracts, schemas, and APIs unless a narrower technical boundary requires otherwise.

| Term | Canonical meaning | Use when |
| --- | --- | --- |
| `workspace` | The top-level durable environment for Marcus | referring to Odin's primary operating environment |
| `initiative` | A durable unit of responsibility | referring to meaningful work that may include a managed project |
| `companion` | A durable role contract | referring to user-facing operating roles |
| `work item` | The durable object that turns intent into execution | referring to bounded work owned by the control plane |
| `run attempt` | One disposable execution attempt for a work item | referring to execution-plane records |
| `control plane` | Odin's persistent state, policy, and dispatch layer | referring to what Odin owns durably |
| `execution plane` | Short-lived worker and executor activity | referring to bounded execution only |

## Narrowed or legacy terms

These terms still have valid uses in the repo, but they are no longer the default product vocabulary.

| Term | Status | Keep using it for | Prefer instead |
| --- | --- | --- | --- |
| `task` | narrowed legacy runtime term | current queue, event, worktree, executor, and observability contracts until the work-item migration lands | `work item` for control-plane work, `run attempt` for execution records |
| `agent` | narrowed legacy registry term | existing registry item kinds, capability catalogs, and older sub-agent references | `companion` for durable roles, `worker` for execution units |
| `scope` | narrowed legacy CLI and routing term | current CLI, session, routing, and event contracts until control-context migration lands | explicit `workspace`, `initiative`, `project`, or `companion` context |
| `command` | narrowed interface term | CLI verbs and authored command definitions in registry contracts | explicit object names for product concepts; do not use as a synonym for work |

## Intentional remaining legacy zones

The grep audit for `README.md` and `docs/contracts/` will still find narrowed terms in existing contracts. Those hits are expected where the current implementation genuinely still uses them, including:

- runtime and observability contracts that still model queue state with `task`
- registry and capability contracts that still expose `agent` and `command` item kinds
- CLI and session contracts that still define explicit `scope` behavior
- Git worktree and project-governance contracts that still describe task-owned mutation paths

Those terms should be treated as implementation-era vocabulary, not the semantic center of the product.

## Writing rules

- Prefer canonical terms in new docs, APIs, projections, and package-level design notes.
- When a narrowed legacy term is unavoidable, pair it with the canonical meaning nearby on first use.
- Do not introduce new parallel abstractions that overlap `workspace`, `initiative`, `companion`, `work item`, or `run attempt` without explicit justification.
