# Phase 23a Family-Ops Shadow Onboarding Design

## Objective

Onboard `family-ops` as a second real managed project on the operational main line in `shadow` mode and verify that Odin can supervise more than one project safely without enabling mutation.

## Scope

This phase stays strictly shadow-only:

- add a conservative `family-ops` manifest entry
- add one focused multi-project shadow test
- run a real shadow onboarding smoke with the CLI and `serve`
- write a concise onboarding audit

This phase does not add limited-action policy, executor behavior, or new runtime authority.

## Approaches Considered

### 1. Manifest-only onboarding plus live smoke

Add the real project to `config/projects.yaml`, then rely on the existing CLI and runtime surfaces for verification.

Pros:

- smallest change set
- closest to the real operator flow

Cons:

- weak automated proof for multi-project scope isolation

### 2. Manifest onboarding plus one focused multi-project shell test

Add the real manifest entry and add one test proving transition/observe/lease surfaces remain correctly scoped when more than one managed project exists.

Pros:

- still small
- gives durable proof for the new multi-project shadow use case

Cons:

- slightly more test surface

### 3. Build a dedicated portfolio-onboarding command

Pros:

- better long-term operator workflow

Cons:

- wrong scope for this phase
- adds product surface instead of validating the current one

## Recommendation

Use approach 2.

The runtime and CLI already support shadow-mode supervision. The missing proof is that these surfaces still behave correctly once there is more than one real managed project on the operational line. One focused multi-project shell test plus a real `family-ops` shadow smoke is enough.

## Design

### Manifest

Keep `config/projects.yaml` canonical and portable.

Load `pbs` and `family-ops` through a machine-local overlay instead:

- `ODIN_PROJECTS_OVERLAY` when explicitly set
- `config/projects.local.yaml` when present on one machine

That keeps main operationally shadow-only without hard-coding machine-local repo roots into canonical config.

### Test Coverage

Add one focused shell test covering a two-project registry:

- switch to project `alpha`
- set `shadow`
- record an observation
- switch to project `family-ops`
- confirm `/transition` starts at default `inventory`
- confirm `/leases` remains empty and scoped

This proves multi-project scope switching does not leak shadow reports or lease visibility.

### Live Verification

Run the real CLI flow on a fresh `ODIN_ROOT`:

1. `/project family-ops`
2. `/transition`
3. `/transition set shadow because observe only`
4. `/observe ...`
5. `/mode act`
6. one bounded smoke task
7. `odin serve`
8. verify:
   - transition denial
   - no worktree lease allocation
   - usable `/jobs`, `/runs`, `/logs`, `/leases`

### Deliverables

- `config/projects.yaml`
- `internal/cli/repl/shell_test.go`
- `docs/audits/phase-23a-second-project-shadow-onboarding.md`

## Success Criteria

- `family-ops` loads cleanly on main as a managed project
- Odin can inspect and supervise it in shadow mode
- Act-created work still fails closed under shadow authority
- no lease/worktree allocation occurs
- operator shell surfaces stay sane across more than one project
