---
kind: workflow
key: flica-fcfs-bid
title: Marcus FLICA FCFS Bid Workflow
summary: Operator-invoked workflow for first-come-first-served FLICA bid or pickup actions using a shared Bid Action model.
status: active
tags:
  - flica
  - bid
  - fcfs
  - tradeboard
  - pbs
  - operator-invoked
owners:
  - odin-core
  - pbs-flight-api
entrypoint: command:/tradeboard pickup
composes:
  - registry:flica-schedule
  - command:/tradeboard trips
  - command:/tradeboard pickup
  - pbs-flight-api
---

# Marcus FLICA FCFS Bid Workflow

## Purpose
Coordinate an operator-approved first-come-first-served Bid Action for Marcus through Odin while leaving airline-domain FCFS semantics and BCID interpretation with PBS. Odin owns the Workflow Run, Prepared Action Payload, Operator Approval, Action Record, and proof requirements.

## When to Use
Use this workflow when the target FLICA opportunity is controlled by first-come-first-served timing and the operator wants Odin to prepare and submit a bid or pickup request. Do not use it for seniority-ranked actions, post-award TradeBoard posts, annual vacation, or schedule-only inspection.

## Inputs
Inputs are the target pairing or activity, active BCID, Trade Evaluation Mode `fcfs`, Schedule Snapshot reference, Schedule Freshness Requirement, Submit Path, Readback Path or Substitute Proof, and any operator-facing comment needed for the Prepared Action Payload. Helper Permissions may allow PBS/flight-api discovery of trips and BCIDs, but helpers must not approve or submit final airline-facing writes.

## Procedure
Start an Operator-Invoked Workflow Run from the Odin Operator Surface. Run Schedule Preflight through `registry:flica-schedule` and confirm the Schedule Snapshot satisfies this workflow's freshness rule. Use `/tradeboard trips` or PBS/flight-api discovery to prepare the target activity and BCID.

Create an immutable Prepared Action Payload with action type `Bid Action`, Trade Evaluation Mode `fcfs`, target activity, BCID, schedule evidence, Submit Path, downstream destination, and any comment. Present that payload for Operator Approval. After approval, submit through `/tradeboard pickup <PAIRING> type=fcfs bcid=<ACTIVE_BCID> confirm` or the declared downstream route. Record the Action Evidence Events and collect External Readback through the declared Readback Path.

## Outputs
Outputs are a Workflow Run, zero or one Bid Action Record, append-only Action Evidence Events for preparation, approval, submission, downstream PBS/flight-api evidence, and External Readback or declared Substitute Proof.

## Constraints
This workflow is Operator-Invoked only. It must complete Schedule Preflight before approval or submission. Operator Approval applies to exactly one Prepared Action Payload. Material payload changes require a new payload and approval. Helpers may prepare, inspect, map, draft, and verify, but must not own approval or final submission. Completion requires the declared Proof Requirement.

## Success Criteria
The FCFS Bid Action reaches the completed Action Lifecycle state only after Odin records the approved Prepared Action Payload, successful downstream submission evidence, and External Readback or valid Substitute Proof. A completed Workflow Run without an Action Record must be labeled as inspection, preflight, failed, or abandoned rather than completed with action.
