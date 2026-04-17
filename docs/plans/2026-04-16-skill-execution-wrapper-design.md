# Skill Execution Wrapper Design

## Goal

Reduce the skill runtime trust surface by replacing direct handler execution with a restricted command wrapper that enforces a tighter execution profile, a fixed handler location policy, and auditable wrapper metadata without pretending to provide kernel-level sandboxing.

## Current State

The skill system now has:

- canonical skill definitions in `registry/skills/*.md`
- shared CRUD and invocation through `internal/skills.Service`
- enforced governance permission checks before mutating handlers run
- lifecycle audit logging plus SQLite runtime events for `odin skills ...`

The remaining trust gap is the handler process boundary:

- `internal/skills/invoke.go` still launches handlers directly with `exec.CommandContext(...)`
- the handler may live anywhere inside the repo as long as it passes path/symlink checks
- the invoked process inherits the ambient environment by default
- lifecycle events say what skill ran, but not whether it used a restricted execution profile

That means Odin now governs *whether* a skill may run, but the handler runtime is still looser than it should be.

## Constraints Driving The Design

The next step must satisfy these constraints:

- keep `registry/skills/*.md` as the only skill source of truth
- avoid introducing a second approval or handler registry
- materially reduce risk without overclaiming host isolation
- stay portable across local development, CI, and homelab use
- remain small enough to ship with strong tests

## Options Considered

### Option 1: Keep direct exec and tighten docs only

Document that handlers are unsafe and rely only on governance permission checks.

Pros:

- zero code churn
- no compatibility risk

Cons:

- leaves the main runtime trust boundary unchanged
- does not reduce inherited-environment exposure
- does not improve operator trust

Rejected because it does not materially harden execution.

### Option 2: Add a separate approved-handler registry

Keep the current skill manifests, but require every handler path or hash to also appear in a second allowlist file.

Pros:

- explicit approval surface
- can support future provenance rules

Cons:

- creates drift between skill metadata and handler approval
- makes CRUD more fragile
- adds operator overhead for every skill change

Rejected because it violates the current single-source-of-truth direction.

### Option 3: Restricted wrapper with structural allowlist

Keep the skill manifest authoritative, but require command handlers to live under an allowlisted subtree and run them through one shared wrapper that controls cwd, env, timeout, and audit metadata.

Pros:

- materially tighter than direct exec
- no second registry
- portable and testable
- honest about not being a true sandbox

Cons:

- still not kernel-enforced isolation
- requires modest refactoring in `internal/skills`

Chosen because it is the smallest honest hardening step.

### Option 4: Full OS sandbox

Run handlers under `bwrap`, `firejail`, containers, or another host isolation layer derived from skill permissions.

Pros:

- strongest technical boundary

Cons:

- platform-sensitive
- larger operational dependency surface
- easy to ship partially and overclaim

Rejected for this phase. It remains a later hardening target.

## Chosen Design

### Core Principle

Skill execution becomes:

- governance-gated by the existing permission and transition model
- structurally constrained by a handler path policy
- operationally constrained by one restricted wrapper
- explicitly documented as *not* a host sandbox

The wrapper is a real hardening layer, but it is not a substitute for kernel isolation.

## Execution Boundary

### Allowed handler locations

Command-backed skill handlers must resolve under:

- `scripts/skills/`

This rule applies after path cleaning and symlink resolution. A handler that stays inside the repo but resolves outside `scripts/skills/` is denied.

This keeps the executable surface narrow and predictable:

- skills use one dedicated repo subtree
- operator review scope is clearer
- random executable files elsewhere in the repo cannot be referenced as skill handlers

### Restricted execution profile

Handlers run through a shared execution wrapper instead of raw `exec.CommandContext(...)`.

The wrapper must:

- run with `cwd` set to the repo root
- preserve timeout enforcement
- pass the request payload through stdin exactly as today
- capture stdout and stderr exactly as today
- scrub the inherited environment
- reintroduce only a small explicit env allowlist needed for portability
- stamp the execution with a stable profile name such as `restricted_command_v1`

### Environment policy

The wrapper should not inherit the caller's full environment.

The first allowed environment set should be intentionally small:

- `PATH` so `#!/usr/bin/env bash` handlers still work
- `TMPDIR` when present, otherwise the host temp default
- `ODIN_ROOT` when present so handlers can intentionally use the runtime root
- explicit Odin skill metadata env vars such as:
  - `ODIN_SKILL_KEY`
  - `ODIN_SKILL_HANDLER`
  - `ODIN_SKILL_EXECUTION_PROFILE`

Everything else is dropped.

This is not filesystem isolation, but it closes the easy leak where arbitrary ambient secrets in the parent shell become visible to skill handlers.

### Invocation behavior

Invocation flow becomes:

1. load the skill snapshot under the shared registry lock
2. resolve governance permission policy
3. resolve and validate the handler path against the allowlisted subtree
4. release the registry lock
5. execute through the restricted wrapper
6. decode the structured response
7. emit lifecycle/audit events with execution profile metadata

The wrapper is operational, not semantic. It does not replace the permission model; it sits below it.

## Observability

The lifecycle event contract should record that the restricted profile was used.

The minimal addition is an `execution_profile` field on skill lifecycle events and SQLite payloads.

That gives operators and tests a direct answer to:

- whether the handler ran through the hardened path
- whether a denied run failed before wrapper start
- whether an allowed run used the expected restricted profile

Stable denial codes should continue to work as they do today.

## Testing Strategy

### Unit tests

- resolve handler path inside `scripts/skills/` succeeds
- resolve handler path outside `scripts/skills/` is denied
- symlinked handlers that escape the allowlisted subtree are denied

### Service tests

- handler executes with repo-root cwd
- inherited secret-like env vars are not visible to the handler
- allowed env vars such as `PATH` and `ODIN_ROOT` remain available when expected
- lifecycle events include `execution_profile`

### End-to-end tests

Using the compiled `odin` binary:

- invoke an allowlisted handler and confirm the response reports repo-root cwd
- invoke a handler that probes for a scrubbed env var and confirm it is absent
- confirm a handler outside `scripts/skills/` cannot be created or invoked
- inspect SQLite skill lifecycle events for the recorded `execution_profile`

## Non-Goals

This phase does not attempt to:

- provide filesystem, network, or process sandboxing
- hash, sign, or externally attest skill handlers
- infer permissions from handler code
- split skills into a second “plugin approval” registry

## Result

After this change, skill execution is still not host-sandboxed, but it becomes materially tighter and more honest:

- only dedicated skill handlers may execute
- ambient environment leakage is reduced
- execution runs through one auditable wrapper
- the event stream makes the hardened profile visible

That is a real hardening step without overstating the trust boundary.
