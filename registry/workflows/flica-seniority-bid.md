---
kind: workflow
key: flica-seniority-bid
title: Marcus FLICA Seniority Bid Workflow
summary: Operator-invoked workflow for seniority-ranked FLICA bid or pickup actions using a shared Bid Action model.
status: active
tags:
  - flica
  - bid
  - seniority
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

# Marcus FLICA Seniority Bid Workflow

## Purpose
Coordinate an operator-approved seniority-ranked Bid Action for Marcus through Odin while leaving airline seniority and bid-category semantics with PBS. Odin records the Workflow Run, Prepared Action Payload, Operator Approval, Action Record, and proof outcome.

## When to Use
Use this workflow when the target FLICA bid or pickup is controlled by seniority ranking rather than first-come-first-served timing. Do not use it for FCFS actions, post-award TradeBoard posts, annual vacation, or schedule-only inspection.

## Inputs
Inputs are the target pairing or activity, active BCID, Trade Evaluation Mode `seniority`, Schedule Snapshot reference, Schedule Freshness Requirement, Submit Path, Readback Path or Substitute Proof, and any operator-facing comment needed for the Prepared Action Payload. Helper Permissions may allow PBS/flight-api discovery of current seniority-eligible targets and BCIDs, but helpers must not approve or submit final airline-facing writes.

## Procedure
Start an Operator-Invoked Workflow Run from the Odin Operator Surface. Run Schedule Preflight through `registry:flica-schedule` and confirm the Schedule Snapshot satisfies this workflow's freshness rule. Use `/tradeboard trips` or PBS/flight-api discovery to prepare the target activity and active BCID.

Create an immutable Prepared Action Payload with action type `Bid Action`, Trade Evaluation Mode `seniority`, target activity, active BCID, schedule evidence, Submit Path, downstream destination, and any comment. Present that payload for Operator Approval. After approval, submit through `/tradeboard pickup <PAIRING> type=seniority bcid=<ACTIVE_BCID> confirm` or the declared downstream route. Record Action Evidence Events and collect External Readback through the declared Readback Path.

## Outputs
Outputs are a Workflow Run, zero or one Bid Action Record, append-only Action Evidence Events for preparation, approval, submission, downstream PBS/flight-api evidence, and External Readback or declared Substitute Proof.

## Constraints
This workflow is Operator-Invoked only. It must not perform local seniority ranking in Odin; PBS owns airline-domain seniority semantics. It must complete Schedule Preflight before approval or submission. Operator Approval applies to exactly one Prepared Action Payload. Material payload changes require a new payload and approval. Completion requires the declared Proof Requirement.

## Success Criteria
The Seniority Bid Action reaches the completed Action Lifecycle state only after Odin records the approved Prepared Action Payload, successful downstream submission evidence, and External Readback or valid Substitute Proof. A completed Workflow Run without an Action Record must be labeled as inspection, preflight, failed, or abandoned rather than completed with action.
