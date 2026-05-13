# Lease Cleanup and Inspection

The operational line now includes lease inspection and cleanup surfaces, even though bounded mutation execution remains disabled.

## Inspect leases

From the Odin shell:

```text
/project <key>
/leases
/leases active
/leases released
/leases all
/leases inspect <lease-id>
```

These views show:

- lease state
- cleanup state
- task id
- run id
- task-owned branch
- worktree path
- repo root

## Cleanup released worktrees

The canonical command path previews cleanup without mutating leases or files:

```text
odin leases cleanup --dry-run
```

The preview reports each lease with a cleanup action and reason, including active
leases, released leases, stale cleanup candidates, dirty worktrees, and unsafe
paths. `odin leases cleanup` without an action is also a dry run.

Destructive cleanup requires an explicit operator action:

```text
odin leases cleanup confirm
```

Released worktrees can be cleaned explicitly:

```text
/project <key>
/leases cleanup confirm
```

Rules:

- cleanup only acts on released, not-yet-cleaned leases
- cleanup respects the current shell scope
- cleanup refuses dirty worktrees by default; force cleanup is reserved for explicit approved internal cleanup calls, not `/leases cleanup confirm`
- top-level `odin leases cleanup --dry-run` is global and read-only
- top-level `odin leases cleanup confirm` is explicit and reuses the same cleanup safety policy as the REPL
- cleaned leases remain visible through `/leases all`
- cleaned worktree paths are removed from disk

## Operational note

These surfaces are safe to use on the operational line because they do not broaden mutation authority.

They improve visibility and hygiene for:

- shadow-mode inspection
- denied mutation attempts
- released worktree cleanup after experimental validation in non-operational branches
