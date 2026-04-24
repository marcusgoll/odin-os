---
kind: workflow
key: managed-project-delivery-workflow
title: Managed Project Delivery Workflow
summary: Guides Odin-native software delivery for managed projects from intake through close while optionally using Spec-Flow-compatible project artifacts as evidence.
status: active
tags:
  - managed-project
  - delivery
  - planning
owners:
  - odin-core
entrypoint: skill:triage-skill
composes:
  - karpathy-guidelines
  - triage-skill
  - triage-agent
---

# Managed Project Delivery Workflow

## Purpose
Define the reusable Odin-owned delivery workflow for Git-governed managed projects, from intake through close, while preserving Odin as the control plane and runtime authority.

## When to Use
Use this workflow when a managed project request requires structured software delivery: clarifying the request, producing a specification, writing an implementation plan, breaking work into tasks, executing through governed work, validating, shipping, and closing with evidence.

## Inputs
The workflow takes the active managed project manifest, current scope, operator request, project policy, relevant memory, existing work-item or run-attempt evidence, selected skills or agents, and any project-local compatibility artifacts such as `specs/<feature>/spec.md`, `specs/<feature>/plan.md`, `specs/<feature>/tasks.md`, or `epics/<slug>/state.yaml`.

## Procedure
Resolve the managed project and policy first, then classify the request through intake. Create or refine an Odin-native specification, use Odin planning documents and planning skills such as `writing-plans` for implementation planning, break the approved plan into governed tasks, execute in worktrees under project policy, validate with targeted tests and real operator paths when applicable, ship through the project's approved merge or release path, and close by recording evidence and follow-up context. When Spec-Flow-compatible artifacts exist, read them as project evidence and optionally emit compatible files only as project-local artifacts; do not run Spec-Flow commands or treat those files as runtime authority.

## Outputs
The workflow outputs a scoped delivery plan, task breakdown, implementation evidence, validation evidence, shipping or merge evidence, updated project memory when useful, and optional Spec-Flow-compatible project artifacts that mirror the Odin-owned delivery state without replacing it.

## Constraints
Do not bypass managed-project governance, branch policy, worktree requirements, approvals, or runtime evidence. Do not run `npx spec-flow`, invoke `spec-cli.py`, import slash-command files as Odin commands, create a Spec-Flow project class, or bidirectionally sync with `.spec-flow/`, `specs/*/state.yaml`, or `epics/*/state.yaml`. Treat compatible artifacts as evidence and project-local outputs, not as Odin's source of truth.

## Success Criteria
The workflow remains valid authored catalog content, operators can inspect Spec-Flow compatibility evidence through project views, and user-visible behavior is proven through real `odin` commands while Odin runtime state remains authoritative. First-class operator selection for registry workflows belongs to a later capability-catalog surface because current `main` does not expose a real `/workflow` command.
