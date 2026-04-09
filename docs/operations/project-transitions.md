# Project Transitions

Odin manages project onboarding with an explicit transition ladder. Transition state is runtime authority, not authored config. Use the interactive shell to inspect and change it.

## Supported states

- `inventory`
- `shadow`
- `compare`
- `limited_action`
- `cutover`
- `decommissioned`

## Inspect state

From the Odin shell in project scope:

```text
/project pbs
/transition
```

`/transition` and `/transition status` show:

- project key
- current transition state
- current controller
- mutation authority
- whether Odin can mutate
- limited-action allowlist
- latest notes when present

If the project has not been initialized in the runtime yet, Odin reports the default effective state:

- `state=inventory`
- `controller=legacy_odin`
- `mutation_authority=legacy_odin`

## Change state

Use:

```text
/transition set <state> [allow=<csv>] [confirm] because <reason...>
```

Rules:

- Every transition change requires `because <reason...>`.
- `limited_action` requires both `allow=<csv>` and `confirm`.
- `cutover` requires `confirm`.
- `decommissioned` requires `confirm`.
- `allow=<csv>` is only valid for `limited_action`.

Examples:

```text
/transition set shadow because observe legacy behavior only
/transition set compare because compare new routing with legacy
/transition set limited_action allow=task_branch_prepare confirm because pilot low-risk proposal work
/transition set cutover confirm because odin now owns project mutation authority
```

Transition changes are audited through the runtime event stream.

## Shadow-safe commands

Use these commands after selecting the project:

```text
/observe <summary...>
/compare <summary...>
```

Rules:

- `/observe` only succeeds in `shadow`
- `/compare` only succeeds in `compare`
- both commands are non-mutating and record auditable transition reports

Examples:

```text
/observe legacy deploy completed with no repo mutation
/compare routing mismatch on review task candidate
```

## Safety model

- `inventory`, `shadow`, and `compare` are read-only
- `limited_action` allows only explicitly allowlisted isolated mutations
- `cutover` and `decommissioned` transfer mutation authority to Odin OS
- transition control remains explicit and auditable

Shadow mode must remain fail-closed for mutation. If Odin does not own mutation authority, queued mutating work fails before branch or worktree allocation.
