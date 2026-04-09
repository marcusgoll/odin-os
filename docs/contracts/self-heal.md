# Self-Heal Contract

## Purpose

Phase 11 defines Odin's deterministic self-heal loop for common runtime faults.

## Rules

- Self-heal is distinct from self-improve.
- Every self-heal action must be deterministic, bounded, and auditable.
- No self-heal action may mutate canonical governance policy.
- Retry behavior must stop at configured limits and escalate instead of looping forever.

## Fault Lifecycle

The self-heal loop evaluates:

1. monitor observations
2. diagnosis rules
3. bounded playbook execution
4. escalation when automatic recovery should stop

## Audit Expectations

Phase 11 adds explicit audit detail for:

- incident resolution
- incident escalation
- recovery action execution

These actions must appear in runtime events and recovery records.
