---
kind: workflow
key: approval-required-external-mutation-workflow
title: Approval Required External Mutation Workflow
summary: Routes high-risk external mutation through approval before execution.
status: active
tags:
  - delivery_profile
  - approval_required
  - external_mutation
owners:
  - odin-core
entrypoint: agent:risk-review-agent
composes:
  - risk-review-agent
  - decision-support-agent
---

# Approval Required External Mutation Workflow

## Purpose
Define the lane for high-risk work that may affect external systems, communications, production, permissions, purchases, or sensitive records and therefore requires explicit approval before execution.

## When to Use
Use this profile when the task title or source evidence indicates destructive, governance, production, social, financial, legal, medical, permission, purchasing, or outbound communication impact.

## Inputs
The profile takes the Work Item, project manifest, requested action, risk evidence, approval policy, latest approval state, and any external-system context needed to decide whether execution may proceed.

## Procedure
Classify the action, persist high-risk intent, request or inspect approval through Odin's approval surface, block dispatch until approval is granted, and resume only through the standard Work Item and Run Attempt path.

## Outputs
The profile outputs explicit governance or destructive intent, an approval request or approval-linked block, risk rationale, and operator-visible status evidence.

## Constraints
Do not execute old work during backfill, infer high-risk permission from vague language, bypass approval state, or mutate external systems from review-only evidence.

## Success Criteria
High-risk work is visibly blocked or approved through Odin approvals, audit provenance records the intent decision, and execution uses the canonical executor seam only after approval.
