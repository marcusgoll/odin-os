---
title: Executor Routing Contract
status: active
date: 2026-04-09
phase: "06"
---

# Executor Routing Contract

`config/executors.yaml` is the authored authority for executor inventory and route policy.

## Config responsibilities

The routing config defines:

- executor inventory
- adapter key
- executor class
- enabled state
- priority
- model reference
- optional route-level model reference overrides
- route preference order
- route fallback order

`config/models.yaml` is the authored provider/model registry. Each model entry
records the provider, provider model id when safe to expose, access class,
adapter, capability tags, supported task classes, token/context limits,
estimated token prices, latency tier, risk tier, blocked task classes, and
whether the model is explicitly allowed for high-risk work.

## Route model

Each route matches by portable task properties:

- `task_kinds`
- `scopes`
- `task_classes`
- `risk_classes`

When a route matches:

1. preferred executors are considered in order
2. the route-level model override or executor `model_ref` is resolved
3. disabled, missing, over-budget, context-incompatible, capability-mismatched,
   task-class-blocked, or high-risk-blocked models are skipped
4. unavailable or incompatible executors are skipped
5. configured fallbacks are considered in order
6. the first healthy capability-compatible executor/model pair wins

## Selection rules

- disabled executors are ignored
- disabled models are ignored
- missing model registry entries fail closed when a model registry is configured
- executors whose class does not match their adapter metadata are rejected
- capability matching uses the portable `TaskSpec`
- broker routes are explicit and not implied automatically
- high-risk task classes and risk classes cannot route to broker/API/external
  models unless that model has an explicit high-risk allowance
- budget and context limits are deterministic selector inputs, not prompt hints
- OpenRouter/Kimi non-smoke routing is allowlist-only. The first enabled
  non-smoke task classes are `frontend_build` and `backend_build` with
  `risk_class=low`; either may arrive through the generic work surface or an
  explicit build work kind.
- low-risk `backend_build` is limited to bounded implementation grunt work:
  local code edits, API or service wiring, fixtures, tests, and deterministic
  scaffolding. It must not include credentials, database migrations against
  live data, production deploys, public publishing, security decisions,
  approval resolution, destructive operations, finance, legal, or medical work.
- elevated `frontend_build` risk, including `medium`, `high`, governance,
  destructive, credential-bearing, approval-resolution, production-deploy, or
  public-publish work, must route through the `elevated-frontend-build`
  Premium Codex lane. OpenRouter and other external APIs are not fallbacks for
  elevated frontend risk.
- elevated `backend_build` risk follows the same rule through
  `elevated-backend-build`: Premium Codex only, no OpenRouter or other external
  API fallback.
- cheap or high-volume OpenRouter routing for other task classes remains future
  policy until those classes are explicitly allowlisted in `config/executors.yaml`
  and proven by tests.
- OpenRouter/Kimi routing remains fixture-only for ordinary execution; the only
  live provider-call seam is `odin provider openrouter smoke ...`
- the OpenRouter invocation seam constructs and redacts fixture requests only;
  live provider smoke tests require the approval-gated operator path, explicit
  live confirmation, and local `OPENROUTER_API_KEY` credential handling

## Notes

- `config/models.yaml` stores model metadata referenced by executors and route
  overrides
- selected provider/model metadata is recorded as `model_routing` run evidence
- operators can read that evidence through
  `odin runs routing --run <run-id> [--json]` or
  `odin runs routing --task <task-id-or-key> [--json]`
- operators can preview the current selector result before dispatch through
  `odin work route-preview --task <task-id-or-key> [--json]`
- route preview is a dry run: it resolves the existing Work Item, derives the
  same portable `TaskSpec` used by dispatch, runs deterministic selector policy,
  and must not create a Run Attempt, lease, artifact, runtime event, or provider
  credential read
- routing remains declarative where possible
- `low-risk-frontend-build` and `low-risk-backend-build` are the only current
  non-smoke OpenRouter routes and must stay narrower than their elevated
  Premium Codex routes
- provider-specific hardcoding in the selector is not allowed
- provider adapters must stay thin and translate native calls into the canonical capability gateway envelope
- provider-specific prompt shaping belongs at the provider edge, not in manifests or the capability gateway
- MCP surfaces should expose capabilities as typed tools backed by the canonical capability descriptors
- provider bridges are transport-layer helpers and are not registered in the executor selector catalog
