---
kind: skill
key: security-review
title: Security Review
summary: Review Odin changes involving secrets, subprocesses, tokens, filesystems, and worker policy.
status: active
version: "1.0.0"
enabled: true
tags:
  - security
  - policy
owners:
  - odin-core
strictness: rigid
applies_to:
  - security
  - review
scopes:
  - global
  - odin-core
  - project
permissions:
  - repo.read
handler_type: command
handler_ref: scripts/skills/registry-skill-stub.sh
timeout_seconds: 15
input_schema:
  type: object
  properties:
    request:
      type: string
output_schema:
  type: object
  properties:
    result:
      type: string
---

# Security Review

## Purpose
Catch security regressions before worker execution, GitHub integration, deployment, or filesystem mutation changes are accepted.

## When to Use
Use for changes touching runners, shims, process execution, GitHub tokens, secrets, worktrees, deployment, approvals, or production data boundaries.

## Inputs
Diff, threat surface, command paths, config, env handling, logs, policies, and relevant tests.

## Procedure
Check least privilege, secret redaction, sandbox mode, non-root worker rules, explicit args, approval gates, and production-secret isolation.

## Outputs
Return findings ordered by severity, affected files, required fixes, and residual risk.

## Constraints
Do not rely on prompt instructions as the only security boundary. Do not approve `danger-full-access` or autonomous merge/deploy behavior.

## Success Criteria
The change has explicit security evidence or a clear blocker before it reaches unattended runtime use.
