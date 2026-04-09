# Limited-Action Policy

This document describes the safe limited-action infrastructure promoted onto the `main`-based promotion branch.

It does not mean bounded mutation is enabled on this line.

## What is available

The operational line now understands bounded-action declarations and explicit action keys.

Supported infrastructure:

- explicit Act-mode syntax:
  - `action:<key> <task title>`
- persisted `task.action_key` in SQLite
- bounded-action declarations in project manifests
- validation for known bounded-action keys

Known bounded-action keys:

- `docs_audit_note`
- `docs_update`
- `repo_hygiene_note`

Supported manifest rule fields:

- `description`
- `path_prefixes`
- `target_path`
- `content_mode`

## What is not enabled on this line

This promotion branch does not enable bounded mutation execution.

If a task uses an explicit bounded action key and the project policy supports it, runtime execution still fails closed with:

```text
action key "<key>" is not enabled on this line
```

That fail-closed behavior is intentional.

## Example authored policy

```yaml
policy:
  limited_actions:
    docs_audit_note:
      description: Create an additive audit note under docs/audits
      path_prefixes:
        - docs/audits/
      content_mode: create_markdown_note
    docs_update:
      description: Append a bounded note to an existing docs file
      path_prefixes:
        - docs/
      target_path: docs/plans/2026-03-27-aviation-tooling-audit-report.md
      content_mode: append_markdown_note
```

## Operational rule

- `pbs` remains `shadow`-only on the operational line
- bounded mutation executor paths remain experimental
- unknown, unauthorized, or unsupported action keys must fail closed
