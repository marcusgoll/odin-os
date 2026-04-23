---
kind: workflow
key: project-intake
title: Project Intake Workflow
summary: Defines the initial workflow for enrolling and understanding managed project work.
status: active
tags:
  - projects
  - intake
owners:
  - odin-core
entrypoint: command:status
composes:
  - karpathy-guidelines
  - triage-skill
  - triage-agent
---

# Project Intake Workflow

## Purpose
Describe a workflow for receiving new project work and turning it into a governed runtime action.

## When to Use
Use this workflow when Odin receives a new project-scoped request or needs to re-establish project context after a wake-up.

## Inputs
The workflow takes the request payload, active project manifest data, scope metadata, and any recorded runtime checkpoints.

## Procedure
Resolve the active project, apply the triage skill, invoke the triage agent if additional routing is needed, and return a next-step plan that fits the current scope and policy bounds.

## Outputs
The output is a normalized intake result containing the chosen scope, recommended next action, and any governance requirements for execution.

## Constraints
Do not bypass project governance, do not treat GitHub as required, and do not mutate runtime authority outside the approved execution path.

## Success Criteria
The workflow produces a reproducible intake result that can be executed or surfaced to the operator without ambiguity.
