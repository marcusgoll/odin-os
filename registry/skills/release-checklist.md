---
kind: skill
key: release-checklist
title: Release Checklist
summary: Verify Odin release readiness without deploying production autonomously.
status: active
version: "1.0.0"
enabled: true
tags:
  - release
  - operations
owners:
  - odin-core
strictness: rigid
applies_to:
  - release
  - deployment
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

# Release Checklist

## Purpose
Provide a repeatable release-readiness check for Odin changes that may be deployed or published.

## When to Use
Use before release tagging, deployment handoff, service installation changes, or production-impacting merges.

## Inputs
Release scope, git state, CI status, build outputs, service config, migration impact, rollback path, and approval status.

## Procedure
Verify clean source, required tests, build, operator smoke, migration safety, config/secrets handling, and human approval gates.

## Outputs
Return readiness status, commands run, blockers, rollback notes, and explicit deploy approval requirements.

## Constraints
Do not deploy production autonomously. Do not treat a local build as release proof when CI or service proof is required.

## Success Criteria
The operator has a concise go/no-go checklist with evidence and unresolved risks.
