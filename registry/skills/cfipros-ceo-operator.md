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
    cfipros_repo_root:
      type: string
    kpi_evidence:
      type: object
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
    kpi_truth:
      type: object
    ceo_packet:
      type: object
---

# CFIPros CEO Operator Routine

## Purpose

Prepare a bounded, approval-gated operating packet for the CFIPros CEO operator
agent so scheduled CEO work can be materialized by Odin with truthful KPI
readback and without taking external business action.

## When to Use

Use this skill when a CFIPros CEO routine trigger creates a daily or weekly CEO
checkpoint for launch health, customer acquisition, revenue readiness, growth
closeout, or CEO packet preparation.

## Inputs

The skill receives the checkpoint request, project key, `cfipros-ceo-operator-agent`
handoff key, workflow key, launch document path, review window, approval
boundary, optional CFIPros repo root, and optional operator-supplied KPI
evidence.

## Procedure

Normalize the request into a CEO operator handoff contract, preserve the
approval boundary, collect read-only KPI source truth from the CFIPros repo, and
return structured evidence that the CFIPros CEO operator agent is the intended
reviewer for the scheduled Work Item.

KPI values must be marked `unmeasured` unless a read-only value is supplied as
KPI evidence. Missing KPI data is not zero. Production DB, PostHog, Stripe,
customer, billing, deploy, and merge actions remain outside this skill unless
the operator supplies explicit read-only evidence for the packet.

## Outputs

The output is a structured CEO packet with the CEO agent key, workflow key,
checkpoint, approval-required flag, external side-effect status, `kpi_truth`,
and the ten CEO packet sections expected by `cfipros-ceo-operator-agent`.

## Constraints

Do not contact customers, send outreach, publish, spend money, change pricing,
change Stripe or billing, deploy, merge, or represent CFIPros externally.

## Success Criteria

The scheduled Work Item carries a reviewable CFIPros CEO operator handoff that
can be inspected, approved, and run through normal Odin skill and review paths
without bypassing CFIPros approval rules.
