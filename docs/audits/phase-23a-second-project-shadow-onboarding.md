# Phase 23a: Second Project Shadow Onboarding

## Scope

This audit covers shadow-only onboarding for `family-ops` on the operational line, with `pbs` restored as a second real managed GitHub-backed project in the same manifest set. No limited-action authority was enabled.

## Changes

- added `pbs` back to `config/projects.yaml` as a conservative GitHub-backed project
- added `family-ops` to `config/projects.yaml` as a conservative GitHub-backed project
- added multi-project shell coverage in `internal/cli/repl/shell_test.go`
- added real-config coverage in `tests/integration/alpha_acceptance_test.go`

## Live Verification

Environment:

- repo branch: `phase-23a-family-ops-shadow`
- fresh runtime root: `/tmp/tmp.RVl7pr8IVg`
- runtime entrypoint: `./bin/odin`

### Health and manifest loading

`./bin/odin doctor --json` returned `healthy` on the fresh runtime root.

The interactive shell successfully resolved both:

- `/project pbs`
- `/project family-ops`

That confirms the manifest loaded both external projects on this branch.

### Transition and observe/compare behavior

Verified through the live CLI:

- `/transition set shadow because observe only` succeeded for `pbs`
- `/observe pbs shadow baseline` succeeded
- `/transition` for `family-ops` initially showed `inventory / legacy_odin`
- `/transition set shadow because observe only` succeeded for `family-ops`
- `/observe family-ops shadow baseline` succeeded
- `/compare compare should reject in shadow` failed closed with:
  - `project transition denied: compare_report reports require state "compare"`

Runtime state after the smoke:

- `family-ops|shadow|legacy_odin`
- `pbs|shadow|legacy_odin`

Recorded reports:

- `shadow_observation | pbs shadow baseline`
- `shadow_observation | family-ops shadow baseline`

### Shadow mutation denial

Queued one bounded smoke task in `family-ops`:

- `family-ops-shadow-smoke-task-20260409-135500`

Then ran `./bin/odin serve`.

Observed result:

- task: `failed`
- run: `failed`
- run summary:
  - `project transition denied: controller "odin_os" does not own mutation authority`

This is the correct shadow-mode outcome.

### Lease and branch safety

Lease surface:

- `/leases all` returned `no leases`
- SQLite `worktree_leases` count was `0`

Git branch surface in `/home/orchestrator/family-ops`:

- `git branch --list 'odin/family-ops/*'` returned nothing

This confirms the runtime denied mutation before branch/worktree allocation.

### Operator surfaces

Post-run shell output stayed coherent:

- `/jobs` showed the failed `family-ops` task
- `/runs` showed the failed `codex_headless` run
- `/logs` showed:
  - `project.transition_changed`
  - `project.shadow_observation_recorded`
  - `project.transition_denied`
  - task/run lifecycle events
- `/leases all` stayed empty

## Assessment

`family-ops` is truly shadow-only on this branch.

The operational line can now supervise more than one real external project without mutation:

- `pbs`
- `family-ops`

The promoted action-key and lease infrastructure did not broaden operational authority here. Main-line behavior remains fail-closed for mutation under shadow state.

## Recommendation

Go for broader shadow-only onboarding of additional projects.

Do not broaden limited-action on the operational line yet.

The portfolio is ready for more read-only supervision, because the operator surfaces, transition model, and fail-closed mutation path all behaved correctly with more than one real project. The same evidence does not justify any broader mutation authority.
