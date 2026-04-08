---
kind: command
key: status-command
title: Status Command
summary: Shows Odin's current scope and high-level runtime state.
status: active
tags:
  - cli
owners:
  - odin-core
command: status
scopes:
  - global
  - odin-core
aliases:
  - stat
---

# Status Command

## Purpose
Provide a canonical command definition for showing the current scope and runtime status.

## When to Use
Use this command when an operator needs to understand where Odin is operating before taking the next action.

## Inputs
The command takes the active scope, current project context if any, and the latest available runtime projection.

## Procedure
Resolve the current scope, gather the relevant projection data, and render the most important status details in operator-facing output.

## Outputs
The output is a status view that summarizes active scope, current project identity, and any immediate warnings or blockers.

## Constraints
Do not mutate runtime state, do not infer hidden scope changes, and do not hide projection gaps.

## Success Criteria
The operator can identify the current scope and decide the next command without additional lookup steps.
