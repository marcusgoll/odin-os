# Phase 22 Safe Infrastructure Promotion Design

## Goal

Promote only the safe limited-action infrastructure from the experimental branch into a `main`-based line without enabling bounded mutation on `main`.

## Constraints

- `pbs` must remain `shadow`-only on the operational line
- no merge authority
- no bounded mutation executor path on the promotion branch
- no experimental `pbs` manifest promotion
- prefer selective reapplication over whole-branch merge

## Promotion boundary

### Safe to promote

- persisted `task.action_key` support in SQLite and runtime events
- manifest parsing and validation for bounded action declarations
- explicit action-key parsing in Act mode
- fail-closed runtime handling for explicit bounded-action tasks
- lease inspection and cleanup operator surfaces
- safe docs that describe the infrastructure boundary and the fact that execution remains disabled

### Must stay experimental

- real bounded mutation execution in `internal/executors/codex/adapter.go`
- `pbs` limited-action policy on the operational line
- pilot and expansion docs that describe successful real bounded mutations on `pbs`
- anything that enables `limited_action` execution to succeed on the promotion branch

## Approaches

### 1. Cherry-pick the experimental commits and revert risky pieces

Fastest, but too error-prone. It is easy to miss a live bounded mutation path or an experimental `pbs` policy change.

### 2. Reapply the inert subset manually on top of `main`

Recommended.

This keeps the promotion boundary explicit and auditable. The branch can gain action-key and lease infrastructure while still failing closed for any bounded-action execution.

### 3. Promote only docs and tests

Too weak. The operational line needs the actual schema, parser, validation, and shell surfaces to benefit from the promotion.

## Recommended design

### Runtime and store

Add `task.action_key` to the schema and task/runtime event model. `CreateTaskFromAct` should parse `action:<key> ...` and persist the explicit key. The operational line should understand the concept without being able to execute bounded mutations.

### Project policy model

Promote the bounded-action declaration model:

- `policy.limited_actions`
- `description`
- `path_prefixes`
- `target_path`
- `content_mode`

This makes policy authored, validated, and inspectable on `main`, but it does not grant runtime mutation authority by itself.

### Execution behavior on the promotion branch

Any explicit bounded action on this branch must still fail closed.

That means:

- `action_key` is parsed and stored
- project manifest support exists
- transition allowlists can name bounded keys
- runtime still returns an explicit failure such as `action key "<key>" is not enabled on this line`

No bounded-action success path should exist in the executor or jobs runtime.

### Lease operator surfaces

Promote:

- `/leases`
- `/leases active`
- `/leases released`
- `/leases all`
- `/leases inspect <lease-id>`
- `/leases cleanup confirm`

These are inert operational surfaces and make shadow-mode inspection stronger without broadening mutation authority.

## Expected outcome

The promotion branch should be stronger and more inspectable than `main`, but it should still behave conservatively:

- explicit bounded-action tasks can be created
- they cannot execute successfully
- released lease cleanup is operator-usable
- `pbs` remains shadow-only on the operational line
