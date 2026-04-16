---
apiVersion: odin/v1
kind: workflow
name: project-status-workflow
version: 1.0.0
availability:
  scope: project
permissions:
  - filesystem
  - web
inputSchema:
  ref: schema://odin/workflows/project-status/input
  type: object
outputSchema:
  ref: schema://odin/workflows/project-status/output
  type: object
dependencies:
  - kind: skill
    name: triage-skill
    version: 1.0.0
  - kind: command
    name: project-status
    version: 1.0.0
execution:
  mode: orchestrated
  timeout: 60s
implementation:
  kind: markdown
  path: internal/registry/testdata/normalized/workflow-project-status.md
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
