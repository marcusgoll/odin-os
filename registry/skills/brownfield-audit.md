---
kind: skill
key: brownfield-audit
title: Brownfield Audit
summary: Inventory current Odin behavior before proposing or changing architecture.
status: active
version: "1.0.0"
enabled: true
tags:
  - brownfield
  - audit
owners:
  - odin-core
strictness: rigid
applies_to:
  - audit
  - refactor
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

# Brownfield Audit

## Purpose
Protect existing Odin behavior by forcing a current-state audit before any refactor, migration, or feature implementation.

## When to Use
Use when work touches existing packages, scripts, registry assets, prompts, deployment files, or runtime behavior.

## Inputs
The request, relevant docs, current git status, affected directories, tests, scripts, and real `odin` command surfaces.

## Procedure
Inspect the existing code and docs first. Separate proven behavior from partial, duplicate, or design-only assets. Classify assets as keep, refactor, replace, remove-later, or reference-only.

## Outputs
Return current state, reused components, gaps, risks, and the smallest safe next change.

## Constraints
Do not dismiss messy behavior without evidence. Do not create a parallel abstraction when an existing Odin seam can be reused.

## Success Criteria
A future worker can make a scoped change without rediscovering the same inventory or breaking known behavior.
