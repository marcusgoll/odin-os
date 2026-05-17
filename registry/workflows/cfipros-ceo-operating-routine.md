---
kind: workflow
key: cfipros-ceo-operating-routine
title: CFIPros CEO Operating Routine
summary: Runs approval-gated CFIPros CEO checkpoints throughout the launch day and routes the packet to the CFIPros CEO operator agent.
status: active
tags:
  - cfipros
  - ceo
  - launch
  - scheduler
owners:
  - odin-core
  - cfipros
entrypoint: agent:cfipros-ceo-operator-agent
composes:
  - cfipros-ceo-operator-agent
  - cfipros-ceo-operator
  - scheduler-created-workflow
---

# CFIPros CEO Operating Routine

## Purpose

Define the Odin-owned lane for recurring CFIPros CEO launch work. The workflow
turns daily and weekly scheduler checkpoints into reviewable CEO operator
handoffs without creating a second scheduler, queue, or external-action bot.

## When to Use

Use this workflow when `odin trigger seed cfipros-ceo-day-routine` creates
scheduled work for CFIPros launch health, customer acquisition, revenue
readiness, growth closeout, or weekly CEO packet preparation.

## Inputs

The workflow receives the automation trigger, schedule checkpoint, CFIPros
managed-project scope, the CFIPros CEO launch plan, KPI availability, approval
boundary, and the target `cfipros-ceo-operator-agent` handoff.

## Procedure

Materialize the scheduled checkpoint as a `skill_invocation` Work Item, attach
the CEO operator handoff evidence, keep execution intent read-only, and leave
all external actions behind normal Odin review and CFIPros workflow gates.

## Outputs

The workflow outputs a reviewable Work Item and, when run, a CEO operator
handoff artifact naming the checkpoint, launch-doc authority, required approval
boundaries, stop conditions, and intended agent.

## Constraints

Do not contact customers, publish, spend money, mutate billing, change prices,
deploy, merge, or represent CFIPros externally. Do not invent KPI values; mark
missing data as `unmeasured`.

## Success Criteria

CFIPros CEO work is discoverable in the registry, schedulable through
`odin trigger seed cfipros-ceo-day-routine`, visible as scheduler-created work,
evidence-producing through `odin skills run`, and bounded by the CEO operator
approval contract.
