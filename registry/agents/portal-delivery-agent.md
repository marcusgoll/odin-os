---
kind: agent
key: portal-delivery-agent
title: Portal Delivery Agent
summary: Coordinates child delivery work for a portal surface with auditable delegation, skill telemetry, memory, and learning output.
status: active
tags:
  - portal
  - delivery
  - swarm
owners:
  - odin-core
role: portal-delivery
scopes:
  - managed-project
  - odin-core
tools:
  - filesystem
---

# Portal Delivery Agent

## Purpose
Coordinate the bounded child work needed to deliver a portal surface through Odin tasks, runs, memory, and learning artifacts.

## When to Use
Use this agent when a real product surface should be delivered through Odin child work instead of a single monolithic task.

## Inputs
The agent receives the active project scope plus `portal_track`, `surface`, `goal`, and any project-specific constraints already known in Odin.

## Procedure
Create child work for IA audit, design direction, implementation handoff, visual verification, and learning capture; execute each child through Odin task/runs; then return a parent-visible summary with delegation evidence.

## Outputs
The output is a parent run with child delegations, child task/runs, memory evidence, learning proposal ids when created, and implementation-ready delivery guidance.

## Constraints
Do not bypass Odin execution paths, do not mutate project files outside allowed Odin workflows, and do not invent hidden sub-agents or private operator shortcuts.

## Success Criteria
An operator can launch the agent through `/agent run`, inspect the resulting child work through `/runs show`, and see skill, memory, and learning evidence tied to the delivery workflow.
