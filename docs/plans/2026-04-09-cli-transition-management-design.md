# CLI Transition Management Design

## Objective

Make project transition management operator-usable from the Odin shell so transition state no longer requires manual SQLite edits.

## Recommended Approach

Add a thin REPL command layer over the existing runtime transition service in `internal/core/projects`.

This keeps the current authority model intact:

- transition state remains in SQLite
- transition checks remain in the service layer
- shell commands only parse operator intent and render results

## Command Surface

Add these commands:

- `/transition`
- `/transition status`
- `/transition set inventory because <reason...>`
- `/transition set shadow because <reason...>`
- `/transition set compare because <reason...>`
- `/transition set limited_action allow=<csv> confirm because <reason...>`
- `/transition set cutover confirm because <reason...>`
- `/transition set decommissioned confirm because <reason...>`
- `/observe <summary...>`
- `/compare <summary...>`

## Scope Rules

Transition commands are valid only in `project` and `odin-core` scope.

They should fail clearly in:

- `global`
- `new-project`

## Safety Rules

- All transition changes require a reason via `because`.
- `limited_action`, `cutover`, and `decommissioned` require explicit `confirm`.
- `limited_action` also requires an explicit allowlist with `allow=<csv>`.
- `/observe` records only in `shadow`.
- `/compare` records only in `compare`.

The shell must not infer `limited_action` allowlists automatically.

## Status Output

`/transition` and `/transition status` should show:

- project key
- transition state
- controller
- mutation authority owner
- whether Odin currently owns mutation authority
- limited actions, when present
- notes, when present

## Runtime Project Creation

If the scoped project exists in the manifest but not yet in the runtime `projects` table, transition commands should create the runtime project row before reading or setting transition state.

This keeps onboarding usable without requiring an Act-mode task first.

## Audit Expectations

No new event model is needed.

The shell should reuse:

- `SetTransitionState`
- `RecordShadowObservation`
- `RecordCompareReport`

Those already append auditable events.

## Testing

Add shell tests for:

- `/help` includes transition commands
- `/transition` shows default inventory state and authority
- valid `shadow` transition set
- invalid risky transition without `confirm`
- invalid `limited_action` without `allow=...`
- `/observe` in `shadow`
- `/compare` in `compare`
- global-scope rejection

