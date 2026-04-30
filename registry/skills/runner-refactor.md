---
kind: skill
key: runner-refactor
title: Runner Refactor
summary: Consolidate runner behavior behind Odin's canonical executor and AgentRunner seams.
status: active
tags:
  - runner
  - codex
owners:
  - odin-core
strictness: rigid
applies_to:
  - runner
  - executor
---

# Runner Refactor

## Purpose
Normalize Codex and agent-runner behavior without unsafe subprocess glue or duplicate execution authority.

## When to Use
Use when changing `internal/runner`, `internal/executors`, Codex CLI execution, app-server experiments, worker launch, timeouts, dry-run behavior, or runner logging.

## Inputs
Runner inventory, executor contract, security policy, worktree path, prompt data, timeout, dry-run flag, and tests.

## Procedure
Find all invocation paths, keep the canonical interface explicit, build commands with args, enforce sandbox policy, redact secrets, and keep app-server deferred unless already proven.

## Outputs
Return the consolidated path, duplicate paths, tests for command construction/dry-run/timeout/redaction, and remaining cleanup tickets.

## Constraints
Do not execute arbitrary issue text as shell commands. Do not use `danger-full-access`. Do not expose production secrets to workers.

## Success Criteria
At least one runner path is consolidated and future runner work has one documented interface to extend.
