# Phase 13 Project Transition Design

## Goal

Implement an explicit transition ladder that moves managed projects from the old Odin controller to Odin OS through auditable, enforceable states without allowing dual-controller mutation.

## Current Context

Odin OS already has:

- managed project manifests under `config/projects.yaml`
- a SQLite runtime authority with append-only audit events
- project scope resolution and system-project rules
- isolated Git worktrees, task-owned branches, and lease enforcement

What it does not have yet is a runtime transition model that answers two operational questions:

1. what stage of migration is each project currently in
2. which controller, if any, currently has mutation authority

Phase 13 should add that model without moving mutable rollout state into authored governance config.

## Approaches Considered

### 1. Transition state in `config/projects.yaml`

This would be easy to inspect, but it is the wrong authority. Transition state changes during rollout and therefore belongs in runtime state, not in authored governance manifests.

### 2. SQLite-backed transition ladder with explicit controller ownership

This keeps mutable rollout state in the canonical runtime store, makes every state change auditable, and allows one enforcement gate to decide whether an action is allowed. This is the recommended approach.

### 3. Full dual-controller coordination with live parity checks

This would integrate deeply with the legacy Odin runtime and compare real actions in both systems. It is too large for this phase and would delay the core authority model.

## Recommendation

Store the current transition state for each project in SQLite, define a narrow transition contract in docs, and enforce action rights through a typed gate that combines:

- current transition state
- current controller ownership
- requested action class
- existing project governance policy

Shadow and compare remain strictly read-only. Limited-action allows only a narrow low-risk mutation class. Cutover is explicit, auditable, and transfers mutation authority cleanly to Odin OS.

## Transition States

Phase 13 should support these runtime states:

- `inventory`
- `shadow`
- `compare`
- `limited_action`
- `cutover`
- `decommissioned`

Semantics:

- `inventory`: Odin OS knows about the project but does not observe or mutate it.
- `shadow`: Odin OS records read-only observations only.
- `compare`: Odin OS records read-only observations and decision comparisons only.
- `limited_action`: Odin OS is the active mutating controller, but only for narrowly allowed low-risk actions.
- `cutover`: Odin OS is the active controller for normal managed mutations.
- `decommissioned`: the legacy controller is retired for this project and Odin OS remains authoritative.

## Controller Ownership

Phase 13 should model controller ownership explicitly rather than inferring it from state names alone.

Supported controller values:

- `legacy_odin`
- `odin_os`

Rules:

- in `inventory`, `shadow`, and `compare`, the mutating controller remains `legacy_odin`
- in `limited_action`, `cutover`, and `decommissioned`, the mutating controller is `odin_os`
- only one controller may have mutation authority at a time
- state changes that move mutation authority must be recorded as explicit transition events

## Action Classes

The enforcement model should operate on action classes, not on ad hoc command strings.

Recommended classes:

- `read_only`
- `isolated_mutation`
- `full_mutation`
- `governance_mutation`
- `destructive_mutation`
- `transition_control`

This keeps the transition ladder separate from provider-specific executor logic and lets the CLI, workers, and future APIs ask one consistent authorization question.

## Enforcement Rules

### Read-only states

`inventory`, `shadow`, and `compare` must allow only `read_only`.

These states must reject:

- worktree creation for mutating tasks
- branch creation for mutating tasks
- run execution that would mutate project state
- governance or destructive changes

### Limited-action state

`limited_action` should allow:

- `read_only`
- explicitly allowlisted `isolated_mutation` actions

It must still reject:

- `full_mutation`
- `governance_mutation`
- `destructive_mutation`
- default-branch mutation
- merges or cutovers that are not explicit transition actions

The allowlist should be stored in the runtime transition record so the operator can see exactly what low-risk actions are enabled for a given project.

### Cutover and decommissioned states

`cutover` and `decommissioned` should allow normal mutation through Odin OS, but still subject to the project's ordinary governance policy:

- system-project rules still apply
- destructive operations still require policy approval
- branch and worktree rules still apply

Phase 13 does not weaken existing project policy. It adds one more gate in front of it.

## Storage Model

Phase 13 should add runtime storage for:

- current transition state per project
- append-only shadow observations
- append-only compare reports

Recommended shapes:

- `project_transitions`
  - `project_id`
  - `state`
  - `controller`
  - `limited_actions_json`
  - `notes`
  - `changed_by`
  - `changed_at`

- `project_transition_reports`
  - `project_id`
  - `report_type` as `shadow_observation` or `compare_report`
  - `summary`
  - `details_json`
  - `recorded_at`

There should be one current transition row per project, but reports remain append-only history.

## Event Model

The runtime event stream should gain explicit transition events such as:

- `project.transition_changed`
- `project.shadow_observation_recorded`
- `project.compare_report_recorded`
- `project.transition_denied`

These events must be written in the same transaction as the row change so cutover and compare decisions remain auditable.

## Compare And Shadow Surfaces

Shadow mode should record structured read-only observations. Compare mode should record structured decision reports.

Recommended compare report fields:

- `legacy_summary`
- `odin_summary`
- `verdict` as `match`, `mismatch`, or `needs_review`
- `rationale`

Phase 13 does not require live legacy integration. The reporting contract should be ready for that later, but the initial implementation can accept structured reports from internal callers and persist them cleanly.

## Integration Points

The transition gate should be reusable from:

- CLI command handlers
- task creation and task execution paths
- worktree and branch allocation paths
- future API handlers

Practically, Phase 13 should add:

- a transition package under `internal/core/projects` or `internal/core/transitions`
- store methods for reading and updating the current transition state
- projection helpers for project portfolio transition status and recent reports

The key design rule is that all mutating execution should ask the same enforcement function before proceeding.

## Testing Strategy

Tests should prove:

1. `inventory`, `shadow`, and `compare` cannot authorize mutation
2. `limited_action` allows only explicit low-risk actions
3. `cutover` changes controller authority cleanly and audibly
4. `decommissioned` preserves Odin OS authority without reopening legacy control
5. shadow and compare report recording is append-only and read-only
6. transition denials are explicit and inspectable

Use unit tests for gate logic and store tests for persistence and audit events.

## Non-Goals

Phase 13 does not include:

- live side-by-side execution against the legacy controller
- automatic cutover decisions
- background migration orchestration
- weakening project governance for the sake of rollout convenience
- support for two simultaneous mutating controllers

## Success Criteria

Phase 13 succeeds when each managed project has an explicit, auditable transition state; read-only modes cannot mutate; limited-action mode remains narrow and inspectable; and cutover transfers authority cleanly without dual-controller ambiguity.
