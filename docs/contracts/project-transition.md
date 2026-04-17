---
title: Project Transition Contract
status: active
date: 2026-04-09
phase: "13"
---

# Project Transition Contract

Project transition state is mutable rollout state. It is stored in SQLite, not in `config/projects.yaml`.

The transition ladder defines how a managed project moves from legacy Odin control into Odin OS without allowing dual-controller mutation.

## Transition states

- `inventory`
- `shadow`
- `compare`
- `limited_action`
- `cutover`
- `decommissioned`

Semantics:

- `inventory`: Odin OS knows the project exists but has no mutation authority.
- `shadow`: Odin OS records read-only observations only.
- `compare`: Odin OS records read-only observations and compare reports only.
- `limited_action`: Odin OS is the active mutating controller, but only for explicitly allowlisted low-risk actions.
- `cutover`: Odin OS is the active controller for normal managed mutations.
- `decommissioned`: the legacy controller is retired and Odin OS remains authoritative.

## Controller ownership

The runtime records one controller value for the current project transition state:

- `legacy_odin`
- `odin_os`

Only one controller may have mutation authority at a time.

By default:

- `inventory`, `shadow`, and `compare` map to `legacy_odin`
- `limited_action`, `cutover`, and `decommissioned` map to `odin_os`

## Action classes

Transition enforcement is evaluated against action classes:

- `read_only`
- `isolated_mutation`
- `full_mutation`
- `governance_mutation`
- `destructive_mutation`
- `transition_control`

The transition ladder does not replace ordinary project governance. It adds a state gate in front of it.

## Enforcement rules

### Read-only states

`inventory`, `shadow`, and `compare` allow `read_only` only.

These states must reject:

- mutable worktree allocation
- task-owned mutation branches
- merges or default-branch mutations
- governance changes
- destructive operations

### Limited-action state

`limited_action` allows:

- `read_only`
- explicitly allowlisted `isolated_mutation` actions only

It must reject:

- `full_mutation`
- `governance_mutation`
- `destructive_mutation`
- unlisted isolated mutations

Approval gates still apply after transition authorization. If a project policy requires approval for governance, destructive, or system-project mutations, the runtime must reject the invocation before the handler runs and record the denial.

### Cutover and decommissioned

`cutover` and `decommissioned` allow normal Odin OS mutation, but all existing project governance rules still apply.

## Report contracts

Phase 13 adds append-only transition reports:

- `shadow_observation`
- `compare_report`

Recommended compare payload fields:

- `legacy_summary`
- `odin_summary`
- `verdict`
- `rationale`

Recommended verdict values:

- `match`
- `mismatch`
- `needs_review`

## Audit expectations

The runtime must append explicit events for:

- `project.transition_changed`
- `project.shadow_observation_recorded`
- `project.compare_report_recorded`
- `project.transition_denied`

Transition changes and report writes must remain inspectable through read-only projections.
