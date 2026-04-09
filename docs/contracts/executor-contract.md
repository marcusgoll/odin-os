---
title: Executor Contract
status: active
date: 2026-04-09
phase: "06"
---

# Executor Contract

The executor layer provides one portable task contract across plan-backed CLI runners, direct APIs, and broker routes.

## Executor classes

- `plan_backed_cli`
- `api_executor`
- `broker_executor`

## Portable task spec

The executor contract accepts a strongly typed `TaskSpec` rather than provider-specific payloads.

Required intent fields:

- task id
- task kind
- scope
- prompt
- metadata
- budget hints
- tool policy
- capability requirements

## Required methods

- `Health`
- `Capabilities`
- `RunTask`
- `ResumeTask`
- `CancelTask`
- `EstimateCost`

## Capability requirements

Capability matching is explicit and portable.

Requirements may express:

- allowed executor classes
- resume support
- cancel support
- tool support
- cost estimate support
- headless plan support
- broker fallback support

## Important rule

Subscription-backed CLIs, APIs, and broker routes share one executor contract but remain distinct by class and capability metadata. They are not interchangeable by default.
