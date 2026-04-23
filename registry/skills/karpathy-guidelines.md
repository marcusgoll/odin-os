---
kind: skill
key: karpathy-guidelines
title: Karpathy Guidelines
summary: Behavioral guardrails that keep inbox work goal-driven, simple, and scoped.
status: active
version: "1.0.0"
enabled: true
tags:
  - inbox
  - execution
  - quality
owners:
  - odin-core
strictness: rigid
applies_to:
  - intake
  - execution
scopes:
  - global
  - odin-core
  - project
permissions:
  - repo.read
handler_type: command
handler_ref: scripts/skills/karpathy-guidelines.sh
timeout_seconds: 15
input_schema:
  type: object
  properties:
    request:
      type: string
    scope:
      type: string
output_schema:
  type: object
  properties:
    brief:
      type: string
    success_criteria:
      type: string
---

# Karpathy Guidelines

## Purpose
Reduce common LLM execution mistakes by forcing explicit assumptions, minimal changes, and verifiable completion criteria before inbox work is closed.

## When to Use
Use this skill when Odin is writing, reviewing, or refactoring code for inbox-driven work and needs a compact guardrail against hidden assumptions or overbuilt solutions.

## Inputs
The skill expects the normalized inbox request, current task title, relevant repo or project context, and any known policy constraints already attached to the task.

## Procedure
State assumptions explicitly before acting. Prefer the minimum implementation that solves the request. Keep edits surgical and traceable to the intake. Before finishing, translate the request into concrete success criteria and verify them.

## Outputs
The output is a goal-driven execution brief: the interpreted request, any blocking assumptions, the minimal proposed action, and the success criteria Odin must satisfy before completion.

## Constraints
Do not silently choose between competing interpretations. Do not add speculative features, abstractions, or unrelated cleanup. Do not mutate adjacent code unless the current task requires it.

## Success Criteria
The inbox task is completed only after Odin can state the requested outcome, the concrete checks it used to verify success, and any unresolved blockers or tradeoffs that prevented completion.
