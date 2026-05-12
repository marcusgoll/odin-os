---
kind: workflow
key: local-deterministic-workflow
title: Local Deterministic Workflow
summary: Routes deterministic local checks and readbacks through Odin-owned commands without external mutation.
status: active
tags:
  - delivery_profile
  - local
  - deterministic
owners:
  - odin-core
entrypoint: command:status
composes:
  - status-command
---

# Local Deterministic Workflow

## Purpose
Define the safe default lane for deterministic local work such as status readbacks, fixture-backed checks, and repo-owned verification commands that do not mutate external systems.

## When to Use
Use this profile when the work can be proven with local commands, local files, fixtures, or read-only runtime state and does not require code edits, approvals, network-side effects, or scheduler materialization.

## Inputs
The profile takes the active project, requested local proof, repository command surface, runtime root, relevant Work Item, and any local fixtures or generated evidence paths.

## Procedure
Resolve the project, run the repo-owned deterministic command, capture the command path and result, record any generated evidence, and report the exact proof surface through the Work Item or operator command that requested the check.

## Outputs
The profile outputs command evidence, local runtime readback, and any fixture-backed proof needed to support the Work Item without starting an external mutation.

## Constraints
Do not write to external services, create pull requests, dispatch live automation, or infer mutation permissions from ambiguous local-check language.

## Success Criteria
The Work Item has local deterministic evidence, operators can inspect the profile through `odin work profiles`, and no external mutation or approval-requiring action occurred.
