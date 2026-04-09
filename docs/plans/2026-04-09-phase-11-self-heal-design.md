# Phase 11 Deterministic Self-Heal Design

## Goal

Add deterministic, policy-bounded self-heal behavior that can detect common runtime faults, run explicit recovery playbooks with retry and cooldown limits, and escalate cleanly when automatic recovery should stop.

## Current Context

The Phase 10 runtime already has the important persistence and observability pieces:

- SQLite is the runtime authority.
- Incidents and recoveries are durable and auditable.
- Events are append-only and typed.
- Operator projections already expose incidents, recoveries, freshness, approvals, and blocked work.
- The doctor and metrics surfaces already summarize degraded conditions.

That means Phase 11 should extend the existing runtime instead of introducing a second resilience subsystem.

## Approaches Considered

### 1. Code-defined monitors, diagnosis rules, and playbooks

This keeps self-heal logic in typed Go packages, makes behavior deterministic and easy to test, and lets recovery actions plug directly into the existing store, event model, and projections.

### 2. Markdown-authored playbooks interpreted at runtime

This would align with Odin's authored registry model, but it adds an interpreter and a larger validation surface before the self-heal contract is stable.

### 3. Generic rule engine

This would be flexible, but it would introduce too much abstraction too early and make fault behavior harder to reason about.

## Recommendation

Phase 11 should use code-defined monitors, diagnosis rules, and playbooks. The contract should still be documented clearly so later phases can move some playbooks into authored assets if that turns out to be worth the complexity.

## Architecture

Add a focused self-heal layer under `internal/runtime/recovery`:

- `monitors.go` evaluates current runtime state and emits fault observations.
- `diagnosis.go` maps observations to explicit fault keys and playbook names.
- `playbooks.go` defines typed recovery playbook contracts and deterministic actions.
- `executor.go` runs one recovery attempt with policy, cooldown, retry, logging, and escalation rules.
- `service.go` coordinates a self-heal cycle over the current runtime snapshot.

This package should consume existing state from:

- `internal/runtime/health`
- `internal/runtime/projections`
- `internal/store/sqlite`
- `internal/telemetry/logs`

It should write back through existing runtime tables and events instead of bypassing them.

## Initial Fault Set

Phase 11 should stay intentionally small. The initial self-heal cycle should support these fault types:

1. `executor_health_stale`
2. `projection_stale`
3. `source_freshness_stale`
4. `queue_pressure_high`
5. `run_failure_repeated`

These are already visible in health or projections, so they can be detected without inventing speculative signals.

## Monitor Model

A monitor should produce a typed observation with:

- fault key
- scope
- severity
- subject identifier such as executor, projection surface, task, or run
- evidence summary
- first seen and observed timestamps

Monitors should be pure evaluations over current state. They do not write data, create incidents, or trigger recovery directly.

## Diagnosis Rules

Diagnosis rules take observations and decide whether Odin should:

- ignore the observation because no playbook is defined
- open or reuse an incident
- execute a specific playbook
- escalate immediately

Diagnosis stays deterministic. No model-generated reasoning is involved.

## Playbook Contract

Each playbook should define:

- `Name`
- `FaultKey`
- `AllowedScopes`
- `MaxRetries`
- `Cooldown`
- `EscalateAfter`
- `Action`

The `Action` should be a deterministic Go function that only performs bounded, auditable runtime operations.

Phase 11 playbooks should be limited to actions such as:

- recording a fresh executor health check
- refreshing projection freshness for a named surface
- recording a recovery checkpoint or wake packet
- annotating incident state and escalation reason

No playbook may mutate project governance, manifests, policies, executor routing config, or other canonical governance state.

## Retry and Cooldown Rules

Retries should be keyed by the stable fault identity, not by transient agent identity. For example:

- executor faults by executor name
- projection faults by projection surface
- repeated run failure by task and run lineage

Execution rules:

- if the cooldown window has not elapsed since the last recovery attempt for the same fault key, do nothing
- if attempts are below the retry limit, run the next bounded attempt
- if attempts exceed the limit, stop retrying and escalate

This prevents infinite loops and keeps behavior replayable.

## Incidents, Recoveries, and Events

Every self-heal action must be auditable. Phase 11 should extend the event model with explicit recovery action detail such as:

- `incident.escalated`
- `recovery.action_executed`
- `recovery.escalated`

The store should append those events transactionally with the matching runtime row updates.

The recovery record should capture:

- playbook name
- fault key
- action name
- attempt number
- result
- escalation reason when applicable

## Operator Surfaces

Phase 11 should reuse existing observability channels:

- structured logs for each self-heal decision and action
- projections showing escalated incidents and recent recoveries
- metrics counts for active recoveries and escalated items
- doctor surfaces reflecting whether a fault has already been retried or escalated

No new opaque debugging surface should be introduced.

## Testing Strategy

Phase 11 should be implemented with TDD around the bounded behavior:

1. monitors only emit defined faults
2. diagnosis selects the correct playbook for known faults
3. cooldown suppresses premature retries
4. retry limit escalates instead of looping
5. recovery actions write incidents, recoveries, and events consistently
6. projections and logs reflect the attempted or escalated recovery

## Non-Goals

Phase 11 does not include:

- self-improvement or code mutation
- dynamic authored playbooks
- policy mutation
- autonomous branch or merge operations
- probabilistic diagnosis

## Success Criteria

Phase 11 is successful when Odin can detect a small set of common runtime faults, execute bounded deterministic playbooks, and escalate visibly instead of retrying forever.
