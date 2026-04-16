---
apiVersion: odin/v1
kind: command
name: project.status
version: 1.0.0
availability:
  scope: global
permissions:
  - filesystem
inputSchema:
  ref: schema://odin/commands/project.status/input
  type: object
outputSchema:
  ref: schema://odin/commands/project.status/output
  type: object
dependencies:
  - kind: skill
    name: triage-skill
    version: 1.0.0
execution:
  mode: local
  timeout: 30s
implementation:
  kind: markdown
  path: registry/commands/project.status.md
---

# Project Status Command

## Purpose
Show the current project state in a concise operator-facing form.

## When to Use
Use this command when the operator needs a quick status readout.

## Inputs
The command takes the active scope and runtime projection.

## Procedure
Collect the current context and render the important details.

## Outputs
A compact status summary with any immediate blockers.

## Constraints
Do not mutate runtime state.

## Success Criteria
The operator can decide the next action without extra lookup.
