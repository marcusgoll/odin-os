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
3. typed recovery decisions
4. bounded playbook execution when the decision mode allows it
5. escalation when automatic recovery should stop

Diagnosis must emit a decision for every observation. Supported decision modes:

- `ignore`: record the diagnosis decision and take no executor action.
- `incident_only`: open or reuse an incident, create no recovery row, and wait for operator attention.
- `playbook`: run a deterministic bounded recovery playbook.
- `approval_required`: open or reuse an incident and stop until a recovery review proposal path is available.
- `escalate`: open or update an escalated incident without retrying.

`wake_packet_invalid` is incident-only. Invalid wake-envelope state should be
visible as an incident with a next action for the operator, not a speculative
automatic repair attempt.

Playbook `ActionResult.Status` is closed to:

- `completed`
- `failed`
- `escalated`

If a playbook returns any other status, Odin records
`recovery.action_executed` with canonical result `failed`, includes
`contract_violation.key=invalid_action_result_status` and the raw status, marks
the recovery failed, escalates the incident, and returns an invalid-status
executor error.

Risky recovery actions must remain review-gated. This slice does not add full
recovery review proposal persistence; until that exists, `approval_required`
decisions must not run an automatic playbook or mutate policy.

## Audit Expectations

Phase 11 adds explicit audit detail for:

- incident resolution
- incident escalation
- recovery action execution

These actions must appear in runtime events and recovery records.
Operator readbacks should preserve enough evidence to answer which fault was
observed, which subject it affected, what decision mode was chosen, what status
resulted, and what the next action is.
