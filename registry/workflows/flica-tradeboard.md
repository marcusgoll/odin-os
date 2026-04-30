---
kind: workflow
key: flica-tradeboard
title: Marcus FLICA TradeBoard Workflow
summary: Operator-invoked workflow for post-award FLICA TradeBoard actions including post, pickup, drop, exchange, split-trip actions, and readback proof.
status: active
tags:
  - flica
  - tradeboard
  - huginn
  - pbs
  - operator-invoked
owners:
  - odin-core
  - pbs-flight-api
entrypoint: command:/tradeboard
composes:
  - registry:flica-schedule
  - registry:flica-tradeboard-split-post
  - command:/tradeboard status
  - command:/tradeboard trips
  - command:/tradeboard credit
  - command:/tradeboard scan
  - command:/tradeboard sync-status
  - command:/tradeboard pickup
  - command:/tradeboard post
  - huginn-browser
  - pbs-flight-api
---

# Marcus FLICA TradeBoard Workflow

## Purpose
Coordinate operator-approved post-award FLICA TradeBoard Actions through Odin while PBS owns the airline trade surface, session behavior, and FLICA integration details. This top-level workflow covers post, pickup, drop, exchange, split-trip, and readback procedures; specialized split-trip posting remains documented in `registry:flica-tradeboard-split-post`.

## When to Use
Use this workflow when Marcus needs to inspect, post, pick up, drop, exchange, split, or verify a FLICA TradeBoard item after award. Use the split-post composed workflow when only selected legs of a pairing should be posted and the whole trip must not be dropped.

## Inputs
Inputs are the requested trade surface operation, post-award target activity, split-trip scope when applicable, active BCID, Schedule Snapshot reference, Schedule Freshness Requirement, Submit Path, Readback Path or Substitute Proof, and operator-facing comment when applicable. Helper Permissions may allow Huginn inspection, PBS/flight-api action preparation, split-leg mapping, and readback verification, but not approval or final airline-facing submission.

## Procedure
Start an Operator-Invoked Workflow Run from `/tradeboard`. Run Schedule Preflight through `registry:flica-schedule` and confirm the Schedule Snapshot satisfies this workflow's freshness rule. Discover TradeBoard state with `/tradeboard status`, `/tradeboard trips`, `/tradeboard credit`, `/tradeboard scan`, or PBS/flight-api as needed.

Create an immutable Prepared Action Payload with action type `TradeBoard Action`, requested trade surface operation, target activity, split scope when applicable, active BCID, schedule evidence, comment, Submit Path, and downstream destination. Present the payload for Operator Approval. After approval, submit through the declared route, such as `/tradeboard post ... confirm` or `/tradeboard pickup ... confirm`. Collect downstream PBS/flight-api evidence and External Readback through the declared Readback Path.

## Outputs
Outputs are a Workflow Run, zero or more TradeBoard Action Records, append-only Action Evidence Events, downstream PBS/flight-api evidence, Huginn/browser evidence when used, and External Readback or declared Substitute Proof.

## Constraints
This workflow is Operator-Invoked only. It must not reuse Bid Action for post-award TradeBoard work. It must complete Schedule Preflight before any airline-facing write. Live FLICA UI interaction, Duo, browser proof, or readback through FLICA UI must use Huginn. Operator Approval applies to exactly one Prepared Action Payload. Do not fall back from a split request to a full-pairing drop. Completion requires the declared Proof Requirement.

## Success Criteria
A TradeBoard Action reaches the completed Action Lifecycle state only after Odin records the approved Prepared Action Payload, downstream submission evidence, and External Readback or valid Substitute Proof. For split-trip posts, readback must confirm the intended split marker, pairing, comment, and posted status.
