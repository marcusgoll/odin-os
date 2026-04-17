# Skill Permission Enforcement Design

## Goal

Make skill `permissions` real inside `odin-os` by enforcing them at the Odin governance boundary during skill invocation, using the existing project policy, transition, and approval model instead of leaving permissions as metadata only.

## Current State

The skill system now has:

- canonical skill definitions in `registry/skills/*.md`
- validated `permissions` fields in the skill contract
- a shared `internal/skills.Service` for CRUD and invocation
- repo-scoped mutation locking
- lifecycle audit logging plus SQLite runtime events for `odin skills ...`

The remaining gap is enforcement:

- `permissions` are validated and audited, but not enforced before handler execution
- `odin skills invoke` can currently run any command-backed skill that passes path and timeout checks
- the runtime does not distinguish read-only skill use from project mutation, governance mutation, or destructive mutation

That leaves the most important risk surface unchanged: command-backed skills can claim a permission model without Odin actually gating execution against project policy or transition state.

## Constraints Driving The Design

The next step must satisfy these constraints:

- avoid claiming host isolation that does not exist
- reuse Odin's existing governance and transition controls instead of creating a second permission system
- keep `registry/skills/*.md` authoritative
- keep the first enforcement pass small enough to ship and test thoroughly
- make denials auditable through the existing lifecycle event stream

## Options Considered

### Option 1: Coarse allowlist only

Interpret a tiny set of read-only permissions and reject everything else.

Pros:

- smallest implementation
- low short-term risk

Cons:

- does not integrate with project policy or transition state
- does not help Odin reason about allowed isolated mutations
- too blunt for real autonomous operation

Rejected because it improves safety only marginally.

### Option 2: Permission-to-governance mapping

Map skill permission strings onto Odin's existing project governance model, then enforce them using current CLI scope, selected project manifest, transition state, and approval requirements.

Pros:

- makes `permissions` real today
- reuses existing policy and transition machinery
- gives auditable allow/deny behavior without overstating host isolation
- keeps the implementation narrow and maintainable

Cons:

- still not OS sandboxing
- requires a small vocabulary and explicit mapping rules

Chosen because it is the smallest honest design that materially improves safety.

### Option 3: Full command sandbox

Add a wrapper runtime with filesystem, network, and process isolation derived from skill permissions.

Pros:

- strongest technical boundary

Cons:

- significantly larger implementation
- platform-sensitive
- easy to overclaim if partial
- would delay the near-term hardening work that Odin actually needs now

Rejected for this phase. It remains a later hardening target.

## Chosen Design

### Core Principle

`permissions` become a governance gate, not a pretend sandbox.

Odin will decide whether a skill may run based on:

- the current CLI scope
- the selected project manifest, if any
- the current project transition state, if any
- the mapped action class implied by the skill permissions
- whether an explicit limited action key is required and allowlisted
- whether project policy requires approval for that class of mutation

If the invocation is denied, Odin must block execution before starting the handler and record the denial through the existing lifecycle event path.

## Permission Vocabulary

The first enforced vocabulary is intentionally small and explicit:

- `repo.read`
- `runtime.read`
- `repo.mutate.isolated:<action_key>`
- `repo.mutate.full`
- `repo.mutate.governance`
- `repo.mutate.destructive`

Rules:

- unknown permission strings are invalid at validation time
- multiple permissions are allowed, but the effective requirement is the most restrictive union of the set
- `repo.mutate.isolated:<action_key>` must carry a non-empty limited-action key

Examples:

- a reporting skill can declare `repo.read`
- a low-risk docs-note skill can declare `repo.mutate.isolated:docs_audit_note`
- a release-prep skill can declare `repo.mutate.full`
- a policy-editing skill can declare `repo.mutate.governance`
- a cleanup/reset skill can declare `repo.mutate.destructive`

## Enforcement Model

### Invocation Context

Skill invocation needs an explicit execution context in addition to `key` and `input`.

The service should receive:

- resolved CLI scope
- selected project key when scope is `project` or `odin-core`
- the matching project manifest when available
- access to the transition service and store for authorization and audit

This keeps permission checks in Odin's runtime path instead of inside individual skill handlers.

### Scope Rules

#### Global scope

Global scope allows read-style skill invocation only.

Allowed:

- `repo.read`
- `runtime.read`

Denied:

- any `repo.mutate.*` permission

Reason:

- global scope has no selected project, so there is no safe place to apply mutation policy or transition checks

#### Project scope and odin-core scope

For `project` and `odin-core` scopes:

- load the selected manifest from the bootstrap registry
- map permissions onto action classes
- authorize isolated/full/governance/destructive mutation through the existing `internal/core/projects.Service`
- check approval requirements with the existing project policy helpers

Mapping:

- read-only permissions map to `read_only`
- `repo.mutate.isolated:<action_key>` maps to `isolated_mutation` with the provided action key
- `repo.mutate.full` maps to `full_mutation`
- `repo.mutate.governance` maps to `governance_mutation`
- `repo.mutate.destructive` maps to `destructive_mutation`

Approval behavior:

- if project policy requires approval for the mapped action class, `odin skills invoke` must reject direct execution
- the denial should explain that approval is required rather than silently degrading behavior

### Effective Permission Resolution

The resolver should compute one invocation policy from the declared permission list.

It must answer:

- is this invocation read-only or mutating
- if mutating, what action class applies
- if isolated, what limited action key is required
- is approval required under the selected project policy

This keeps the enforcement logic centralized and testable.

## Observability

Permission enforcement must extend the existing skill lifecycle audit path.

Required outcomes:

- allowed invocation continues to emit the existing lifecycle event with `outcome=success`
- denied invocation emits `outcome=failure`
- denial payload includes a stable `error_code`
- denial text explains whether the reason was:
  - unknown permission
  - mutation in global scope
  - missing project context
  - transition denial
  - limited action not allowlisted
  - approval required

This keeps the runtime event stream useful for debugging and operator trust.

## Testing Strategy

### Unit tests

- permission parsing and normalization
- unknown permission rejection
- isolated-mutation key parsing
- union behavior for multiple permissions

### Service tests

- allow read-only invoke in global scope
- deny mutating invoke in global scope
- allow isolated mutation only when the action key is allowlisted in limited-action state
- deny isolated mutation when the action key is not allowlisted
- deny governance or destructive mutation when approval is required

### End-to-end tests

Using the compiled `odin` binary:

- select project scope
- invoke an allowed read-only skill
- invoke a denied mutating skill in global scope
- invoke a denied isolated-mutation skill when the project is not in the right transition state or allowlist
- confirm SQLite records the deny/allow lifecycle events

## Non-Goals

This phase does not attempt to:

- provide OS-level filesystem, network, or process sandboxing
- auto-request and resolve approvals for denied skill invocations
- infer permissions from handler code
- support arbitrary free-form permission strings

## Result

After this change, skill permissions stop being decorative metadata and become real Odin-governed execution constraints. The system will still not be sandboxed at the host level, but it will enforce skill execution against the same project policy and transition model that Odin already uses for other mutation decisions.
