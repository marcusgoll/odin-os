---
kind: workflow
key: flica-annual-vacation
title: Marcus FLICA Annual Vacation Workflow
summary: Draft operator-invoked workflow contract for airline annual vacation requests, changes, checks, and readback proof.
status: draft
tags:
  - flica
  - vacation
  - schedule
  - huginn
  - pbs
  - operator-invoked
owners:
  - odin-core
  - pbs-flight-api
entrypoint: operator-surface:flica-annual-vacation-required
composes:
  - registry:flica-schedule
  - huginn-browser
  - pbs-flight-api
---

# Marcus FLICA Annual Vacation Workflow

## Purpose
Define the safety and proof contract for operator-approved airline annual vacation actions. Odin owns the Workflow Run, Prepared Action Payload, Operator Approval, Action Record, and proof requirements; PBS owns airline-domain vacation semantics and any future FLICA integration behavior.

## When to Use
Use this workflow when Marcus needs to inspect, request, bid, change, verify, or read back pilot annual vacation status in FLICA. Until a concrete Odin Operator Surface is implemented and declared, this workflow is documentation-only and must not be used for live airline-facing vacation writes.

## Inputs
Inputs are the requested vacation operation, vacation target period or status target, Schedule Snapshot reference, Schedule Freshness Requirement, Submit Path, Readback Path or Substitute Proof, and operator-facing comment when applicable. Helper Permissions may allow local preparation, schedule-impact review, PBS/flight-api reads, and Huginn inspection when declared, but not approval or final airline-facing submission.

## Procedure
Start an Operator-Invoked Workflow Run only after a concrete Odin Operator Surface exists for this workflow. Run Schedule Preflight through `registry:flica-schedule` and confirm the Schedule Snapshot satisfies this workflow's freshness rule. Prepare an immutable Prepared Action Payload with action type `Annual Vacation Action`, requested operation, target vacation period or status target, schedule evidence, Submit Path, downstream destination, and comment when applicable.

Present the payload for Operator Approval. After approval, submit only through the registry-declared Submit Path. Collect downstream evidence and External Readback through the declared Readback Path. If FLICA provides no readable vacation confirmation surface, the Workflow Registry Entry must declare Substitute Proof with the unavailable-readback reason, alternate evidence, and operator-facing risk.

## Outputs
Outputs are a Workflow Run, zero or more Annual Vacation Action Records, append-only Action Evidence Events, schedule-impact evidence, downstream PBS/flight-api evidence when available, Huginn/browser evidence when used, and External Readback or declared Substitute Proof.

## Constraints
This workflow is Operator-Invoked only. It must not treat airline annual vacation actions as generic Schedule Workflow updates or personal calendar events. It is blocked for live airline-facing writes until a concrete Odin Operator Surface and Submit Path are implemented and declared. Operator Approval applies to exactly one Prepared Action Payload. Completion requires the declared Proof Requirement.

## Success Criteria
For inspection-only use, the Workflow Run records an inspected, preflighted, failed, or abandoned Workflow Run Outcome without implying a completed airline-facing action. For future live writes, an Annual Vacation Action reaches the completed Action Lifecycle state only after Odin records the approved Prepared Action Payload, downstream submission evidence, and External Readback or valid Substitute Proof.
