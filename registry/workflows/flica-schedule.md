---
kind: workflow
key: flica-schedule
title: Marcus FLICA Schedule Workflow
summary: Operator-invoked workflow for producing and validating the Schedule Snapshot used by Marcus FLICA workflow preflight checks.
status: active
tags:
  - flica
  - schedule
  - huginn
  - pbs
  - operator-invoked
owners:
  - odin-core
  - pbs-flight-api
entrypoint: command:/tradeboard sync-status
composes:
  - command:/tradeboard sync-status
  - command:/tradeboard scan
  - huginn-browser
  - pbs-flight-api
---

# Marcus FLICA Schedule Workflow

## Purpose
Produce or validate the canonical Schedule Snapshot used by Schedule Preflight for Marcus FLICA workflows. Odin owns the operator invocation, Workflow Run evidence, Schedule Freshness Requirement, and proof requirements; PBS owns airline schedule semantics, FLICA sync behavior, and browser/session integration.

## When to Use
Use this workflow before any live Seniority Bid, FCFS Bid, TradeBoard, or Annual Vacation write, and whenever Marcus needs to inspect current FLICA schedule state before deciding whether an airline-facing action is still valid. Use it for inspection-only or preflight-only runs as well as for supporting another workflow's Prepared Action Payload.

## Inputs
Inputs are the operator request, target workflow needing schedule evidence, schedule period or target activity when known, the workflow-specific Schedule Freshness Requirement, current FLICA Sync status, and any existing Schedule Snapshot reference. Helper Permissions may allow PBS/flight-api status reads and Huginn readback, but not approval or final airline-facing submission.

## Procedure
Start from the declared Odin Operator Surface. Check FLICA Sync status with `/tradeboard sync-status`; if the sync is stale, failing, or running, stop before approval of any downstream airline-facing action. Refresh schedule or TradeBoard-backed FLICA state with `/tradeboard scan headless=true` only when the registry-declared freshness rule requires it.

When live FLICA UI inspection or authentication is required, use the Huginn submit/readback route and the AA SSO start URL. Treat Duo as operator-attended authentication. Record the Workflow Run Outcome as inspected, preflighted, failed, or abandoned when no airline-facing Action Record is created.

## Outputs
Outputs are a Workflow Run, a Schedule Snapshot reference or failure reason, the evaluated Schedule Freshness Requirement, helper activity evidence, and any Huginn or PBS/flight-api evidence used to validate the snapshot.

## Constraints
This workflow is Operator-Invoked only. It must not submit an airline-facing write. It must not define separate schedule semantics outside PBS. It must not approve another workflow's Prepared Action Payload unless that workflow separately records Operator Approval. If the snapshot cannot satisfy the registry-declared Schedule Freshness Requirement, dependent workflows must stop before approval or submission.

## Success Criteria
The Workflow Run records a successful schedule inspection or preflight outcome, the Schedule Snapshot satisfies the requested workflow's Schedule Freshness Requirement, and any dependent workflow can reference that snapshot in its Prepared Action Payload and Action Record evidence.
