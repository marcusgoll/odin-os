---
kind: workflow
key: review-only-workflow
title: Review Only Workflow
summary: Keeps ambiguous, investigative, or approval-shaping work in a non-mutating review lane.
status: active
tags:
  - delivery_profile
  - review
  - read_only
owners:
  - odin-core
entrypoint: agent:review-agent
composes:
  - review-agent
  - pr-review
---

# Review Only Workflow

## Purpose
Define the safe default lane for review, inspection, gap analysis, and clarification work where Odin should gather evidence before any mutation is authorized.

## When to Use
Use this profile when the request is vague, asks for review or analysis, lacks acceptance criteria for code changes, or must produce a recommendation before implementation.

## Inputs
The profile takes the Work Item title, project manifest, current runtime evidence, relevant docs, registry content, and any approval or review context already attached to the work.

## Procedure
Resolve scope, inspect the existing operator surfaces, compare the request against current evidence, identify gaps and risks, and return a review artifact or next-step recommendation without mutating project state beyond audited Work Item evidence.

## Outputs
The profile outputs a review summary, supporting evidence, open questions, and recommended next actions that can be promoted into implementation work only through the governed work surface.

## Constraints
Do not edit code, execute external mutations, dispatch automation, or treat a review conclusion as approval to act.

## Success Criteria
The operator receives a clear read-only review artifact, fallback intent is avoided for active work, and any later mutation must enter an explicit mutation profile.
