---
apiVersion: odin/v1
kind: skill
name: triage-skill
version: 1.0.0
availability:
  scope: global
permissions:
  - filesystem
  - web
inputSchema:
  ref: schema://odin/skills/triage-skill/input
  type: object
outputSchema:
  ref: schema://odin/skills/triage-skill/output
  type: object
dependencies:
  - kind: agent
    name: triage-agent
    version: 1.0.0
execution:
  mode: local
  timeout: 30s
implementation:
  kind: markdown
  path: internal/registry/testdata/normalized/skill-triage.md
---

# Skill Triage

## Purpose
Sort incoming work deterministically.

## When to Use
Use this skill when intake needs classification before deeper work starts.

## Inputs
User request, active scope, and known constraints.

## Procedure
Classify the request and identify the next action.

## Outputs
A routing decision and any blocking questions.

## Constraints
Keep the decision bounded and auditable.

## Success Criteria
Another worker can act without reinterpreting the intake.
