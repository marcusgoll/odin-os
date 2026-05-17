---
kind: skill
key: cfipros-ceo-operator
title: CFIPros CEO Operator Routine
summary: Prepares approval-gated CEO operating requests for the CFIPros CEO operator agent throughout the launch day.
status: active
version: "1.0.0"
enabled: true
tags:
  - cfipros
  - ceo
  - launch
  - growth
  - revenue
owners:
  - odin-core
  - cfipros
strictness: rigid
applies_to:
  - launch-review
  - ceo-packet
  - customer-acquisition
  - revenue-readiness
scopes:
  - project
permissions:
  - repo.read
  - runtime.read
handler_type: command
handler_ref: scripts/skills/cfipros-ceo-operator.sh
timeout_seconds: 15
input_schema:
  type: object
  properties:
    request:
      type: string
    agent_key:
      type: string
    workflow_key:
      type: string
    checkpoint:
      type: string
    approval_boundary:
      type: string
output_schema:
  type: object
  properties:
    result:
      type: string
    agent_key:
      type: string
    workflow_key:
      type: string
    approval_required:
      type: boolean
---

# CFIPros CEO Operator Routine

## Purpose

Prepare a bounded, approval-gated operating request for the CFIPros CEO operator
agent so scheduled CEO work can be materialized by Odin without taking external
business action.

## When to Use

Use this skill when a CFIPros CEO routine trigger creates a daily or weekly CEO
checkpoint for launch health, customer acquisition, revenue readiness, growth
closeout, or CEO packet preparation.

## Inputs

The skill receives the checkpoint request, project key, `cfipros-ceo-operator-agent`
handoff key, workflow key, launch document path, review window, and approval
boundary.

## Procedure

Normalize the request into a CEO operator handoff contract, preserve the
approval boundary, and return structured evidence that the CFIPros CEO operator
agent is the intended reviewer for the scheduled Work Item.

## Outputs

The output is a structured handoff record with the CEO agent key, workflow key,
checkpoint, approval-required flag, and external side-effect status.

## Constraints

Do not contact customers, send outreach, publish, spend money, change pricing,
change Stripe or billing, deploy, merge, or represent CFIPros externally.

## Success Criteria

The scheduled Work Item carries a reviewable CFIPros CEO operator handoff that
can be inspected, approved, and run through normal Odin skill and review paths
without bypassing CFIPros approval rules.
