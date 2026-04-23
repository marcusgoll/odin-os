---
title: Operator Repo Authority Memory
status: active
updated: 2026-04-18
tags:
  - repo
  - migration
  - governance
---

# Operator Repo Authority Memory

## Durable Facts

- `odin-os` is the canonical runtime root and primary implementation target for new Odin work.
- `odin-orchestrator` is migration context only and is being phased out.
- New Odin features should be added to `odin-os` unless the task explicitly targets legacy migration work.

## Working Rule

Audit `odin-os` first, reuse its live contracts and CLI surfaces, and only reference `odin-orchestrator` when migration context is explicitly required.
