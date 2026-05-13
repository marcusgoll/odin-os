---
kind: workflow
key: codex-code-workflow
title: Codex Code Workflow
summary: Routes bounded code changes through the canonical executor seam and project-owned verification.
status: active
tags:
  - delivery_profile
  - codex
  - code
owners:
  - odin-core
entrypoint: skill:go-orchestration-feature
composes:
  - go-orchestration-feature
  - karpathy-guidelines
  - triage-agent
---

# Codex Code Workflow

## Purpose
Define the default mutation lane for bounded repository work that can be performed by Codex through Odin's existing Work Item, Run Attempt, executor, evidence, and verification surfaces.

## When to Use
Use this profile when the request clearly asks for implementation, repair, refactor, documentation mutation, tests, or another repository-scoped change that does not require external real-world mutation approval.

## Inputs
The profile takes the project manifest, Work Item, branch and worktree policy, relevant code and docs, acceptance criteria, executor routing policy, and repo-owned verification commands.

## Procedure
Resolve policy, prepare the allowed workspace, implement the bounded change, keep provenance in the Work Item and Run Attempt, run the relevant repo-owned proof path, and surface the resulting evidence to the operator.

## Outputs
The profile outputs code or documentation changes, test evidence, command evidence, review notes, and any commit or pull request metadata created through the project's approved delivery path.

## Constraints
Do not create a parallel executor model, bypass branch/worktree policy, skip repo-owned verification, or use this profile for production deploys, financial/legal/medical changes, communications, purchases, or permission changes.

## Success Criteria
The Work Item has explicit mutation intent, execution flows through the canonical executor seam, and the operator can verify the result from Odin-owned status and run surfaces.
