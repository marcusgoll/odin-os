---
kind: workflow
key: scheduler-created-workflow
title: Scheduler Created Workflow
summary: Materializes scheduled or trigger-created work with explicit intent while keeping execution governed.
status: active
tags:
  - delivery_profile
  - scheduler
  - trigger
owners:
  - odin-core
entrypoint: agent:automation-candidate-finder-agent
composes:
  - automation-candidate-finder-agent
  - next-action-finder-agent
---

# Scheduler Created Workflow

## Purpose
Define the lane for work that is created by schedules, triggers, or automation intake so materialization is explicit and execution remains separately governed.

## When to Use
Use this profile when a scheduler, trigger, or automation rule creates a Work Item from durable project policy or reviewed intake rather than from a direct operator command.

## Inputs
The profile takes the trigger definition, schedule or source event, project manifest, dedupe key, execution intent metadata, created Work Item, and any linked evidence from intake or review.

## Procedure
Materialize or reconcile the Work Item, persist the source and explicit intent, attach source evidence, leave execution to the normal dispatch and approval gates, and report the item through work status and overview.

## Outputs
The profile outputs a queued or blocked Work Item with source evidence, dedupe provenance, explicit intent, and no implicit run attempt.

## Constraints
Do not auto-execute newly materialized scheduled work, skip dedupe, or infer high-risk mutation authority from trigger text alone.

## Success Criteria
Scheduled work is visible with explicit intent and provenance, dispatch remains operator-governed, and fallback intent is only used for unknown legacy records.
