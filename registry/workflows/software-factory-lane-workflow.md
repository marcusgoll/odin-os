---
kind: workflow
key: software-factory-lane-workflow
title: Software Factory Lane Workflow
summary: Produces managed-project software from reviewed intake or operator start through PR merge when green using existing Odin work, review, approval, run, and merge-gate surfaces.
status: active
tags:
  - managed-project
  - delivery
  - delivery_profile
  - factory_lane
owners:
  - odin-core
entrypoint: command:factory
composes:
  - managed-project-delivery-workflow
  - codex-code-workflow
  - review-only-workflow
---

# Software Factory Lane Workflow

## Purpose
Define the Odin-owned managed-project delivery profile for software factory work, from explicit operator start or reviewed intake promotion through closeout evidence, while keeping existing Odin work, review, approval, run, and merge-gate surfaces as runtime authority.

## When to Use
Use this profile when a managed software project is ready for governed delivery through the factory lane and the requested autonomy boundary is no broader than merge when green.

## Inputs
The profile takes the managed project manifest, admitted work item or reviewed intake item, operator request, project policy, autonomy boundary, branch and worktree policy, risk classification, existing review evidence, PR or merge-gate evidence, and repo-owned verification commands.

## Procedure
Admit the work into the managed project, then move through the standard phase sequence: admitted, specification, implementation_plan, implementation, verification, review, pr_handoff, green_check_wait, merge, and closeout. Each phase records evidence on existing Odin work items, run attempts, review entries, approvals, and PR handoff state instead of creating a separate queue, factory runtime, or provider-specific worker.

## Outputs
The profile outputs factory phase evidence, specification and implementation-plan artifacts when needed, implementation and verification evidence, review findings, PR handoff or merge-gate state, merge evidence when all gates are green, and final closeout evidence for the managed project.

## Constraints
Do not bypass managed-project governance, branch policy, worktree requirements, approval policy, review state, or repo-owned verification. Do not use this profile as a deployment lane, a separate queue, a new runtime authority, or a provider-specific worker contract. The v1 autonomy limit is merge when green; broader production deployment or external mutation remains out of scope unless another approved profile governs it.

## Success Criteria
The profile is active authored registry content tagged as a managed-project delivery profile, operators can inspect it through existing delivery-profile registry surfaces, and factory delivery state remains derived from Odin work items, run attempts, approvals, review entries, PR handoff, merge gates, and closeout evidence.
