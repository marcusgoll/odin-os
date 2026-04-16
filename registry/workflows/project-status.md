---
kind: workflow
key: project-status-workflow
title: Project Status Workflow
summary: Coordinates project intake and status gathering.
status: active
tags:
  - projects
  - status
owners:
  - odin-core
entrypoint: command:project.status
composes:
  - triage-skill
  - triage-agent
---

# Project Status Workflow

## Purpose
Coordinate intake and status gathering for a project.

## When to Use
Use this workflow when project context needs to be re-established.

## Inputs
Request details, current project metadata, and runtime checkpoints.

## Procedure
Classify the request, gather context, and produce a next-step plan.

## Outputs
A normalized intake result and any governance requirements.

## Constraints
Preserve project policy and avoid unnecessary mutation.

## Success Criteria
The workflow yields a reproducible next-step plan.
