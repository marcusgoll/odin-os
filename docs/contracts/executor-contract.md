---
title: Executor Contract
status: active
date: 2026-04-09
phase: "06"
---

# Executor Contract

The executor layer provides one portable task contract across harness-driver-backed headless CLIs, direct APIs, and broker routes.

## Executor classes

- `plan_backed_cli`
- `api_executor`
- `broker_executor`

In the current alpha cutover, `plan_backed_cli` means a durable headless lane that delegates to an external harness driver such as Codex or Claude Code. Odin prepares the `TaskSpec`, selects the route, and records runtime state; the harness driver owns the interactive agent session.

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

Harness-driver executors receive that task spec as structured input on stdin and must return structured status, output, and external id data on stdout.

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

## Harness-driver rules

- headless CLI executors are unavailable until their driver command environment variable is configured
- route selection may prefer headless lanes, but runtime execution must fail explicitly when no configured headless driver satisfies the route
- API and broker executors remain distinct classes; they are not substitutes for a required harness-driver lane

## Important rule

Subscription-backed CLIs, APIs, and broker routes share one executor contract but remain distinct by class and capability metadata. They are not interchangeable by default.
