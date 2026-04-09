---
title: Phase 06 Executor Architecture Design
status: accepted
date: 2026-04-09
phase: "06"
---

# Phase 06 Executor Architecture Design

## Goal

Introduce a model-agnostic executor layer so Odin can route the same task contract across headless CLI runners, direct APIs, and broker routes without changing task payload shape.

## Chosen Approach

Phase 06 will use:

- one portable executor contract
- explicit executor classes
- declarative route configuration
- skeletal provider adapters

This keeps routing policy authored and observable, while preserving the class differences between subscription-backed CLIs, direct APIs, and broker lanes.

## Rejected Alternatives

### Provider-first routing logic

Rejected because it would encode provider behavior directly in the router and make fallback selection harder to audit and change.

### Separate task contracts per executor class

Rejected because task portability is the point of this phase. CLI, API, and broker executors should differ in capabilities and routing metadata, not in the portable task spec.

### Full provider implementation in this phase

Rejected because Prompt 06 is about the abstraction, route selection, and adapter boundaries. Live provider execution can land in later phases on top of this contract.

## Executor Contract

The common contract lives under `internal/executors/contract`.

It should define:

- `Executor`
- `TaskSpec`
- `TaskHandle`
- `ExecutionResult`
- `HealthReport`
- `Capabilities`
- `CostEstimate`
- `ResumePacket`

Required executor methods:

- `Health`
- `Capabilities`
- `RunTask`
- `ResumeTask`
- `CancelTask`
- `EstimateCost`

The contract must stay executor-neutral. Provider-specific flags do not belong in `TaskSpec`.

## Portable Task Spec

`TaskSpec` should carry only portable execution intent:

- task id
- task kind
- scope
- prompt or instruction input
- budget hints
- tool policy
- metadata
- capability requirements

Capability requirements should express what the task needs, such as:

- allowed executor classes
- resume support
- cancel support
- tool access
- cost estimate support

## Executor Classes

Phase 06 introduces three classes:

- `plan_backed_cli`
- `api_executor`
- `broker_executor`

These classes share the same contract but remain distinct in routing and capability reporting.

This is important because subscription-backed CLI runners and APIs are not operationally identical even when they implement the same `RunTask` method.

## Provider Adapters

Each adapter package should provide a compile-ready skeleton that exposes:

- stable adapter key
- executor class
- static capabilities
- health shape
- not-yet-implemented execution methods

Required adapter skeletons:

- `codex_headless`
- `claude_code_headless`
- `gemini_cli_headless`
- `openai_api`
- `anthropic_api`
- `google_api`
- `xai_api`
- `openrouter_api`

Suggested class mapping:

- `codex_headless`, `claude_code_headless`, `gemini_cli_headless` -> `plan_backed_cli`
- `openai_api`, `anthropic_api`, `google_api`, `xai_api` -> `api_executor`
- `openrouter_api` -> `broker_executor`

## Routing Model

The router lives under `internal/executors/router`.

Inputs:

- portable `TaskSpec`
- authored route config from `config/executors.yaml`
- registered adapters
- health reports

Outputs:

- selected executor
- route decision metadata
- fallback reason chain

Routing should remain declarative where possible.

The router should:

1. load enabled executors from config
2. match a route rule to the task
3. filter executors by capability requirements
4. reject unhealthy or unavailable executors
5. choose the first acceptable configured primary
6. fall through to configured fallback executors when needed

## Config Model

`config/executors.yaml` becomes the authored route authority.

It should declare:

- executor inventory
- adapter key
- executor class
- enabled state
- priority
- route rules
- fallback order

`config/models.yaml` can hold model metadata referenced by executors, but route selection remains rooted in `config/executors.yaml`.

## Testing Strategy

Phase 06 tests should focus on contract safety and router behavior:

- capability matching against task requirements
- route rule matching by task kind and scope
- primary executor preference
- fallback when a primary is unavailable
- fallback when a primary lacks required capabilities
- stable task portability across executor classes

Provider adapters should be tested only for stable metadata and class reporting in this phase.

## Phase Boundary

Phase 06 introduces:

- the common executor contract
- executor classes
- declarative routing config
- skeletal adapters
- router selection logic

Phase 06 does not yet introduce:

- live provider integration
- shell or worker execution through the new router
- cost accounting persistence
- run resumption from remote providers
