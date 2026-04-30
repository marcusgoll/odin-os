---
kind: skill
key: architecture-plan
title: Architecture Plan
summary: Produce an incremental architecture plan grounded in current Odin seams.
status: active
version: "1.0.0"
enabled: true
tags:
  - architecture
  - planning
owners:
  - odin-core
strictness: rigid
applies_to:
  - architecture
  - migration
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

# Architecture Plan

## Purpose
Map a proposed architecture change onto existing Odin packages, contracts, state, and command surfaces.

## When to Use
Use when a change may affect runtime composition, SQLite authority, executor routing, worktree isolation, registry assets, or operator commands.

## Inputs
Current code inventory, target behavior, architecture docs, ADRs, dependency graph, and known risks.

## Procedure
Identify the existing ownership boundary, compare current and target state, choose reuse points, isolate high-risk changes, and order the migration into vertical slices.

## Outputs
Return component mapping, migration phases, risk ranking, verification gates, and ADR needs.

## Constraints
Do not propose a big-bang rewrite. Do not bypass `internal/app/lifecycle`, `internal/store/sqlite`, `internal/executors`, or `internal/vcs` without a documented reason.

## Success Criteria
The plan explains how Odin gets from current to target behavior without breaking existing operator workflows.
