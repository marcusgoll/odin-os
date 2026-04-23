---
title: Skill Lifecycle Contract
status: active
date: 2026-04-16
phase: "17"
---

# Skill Lifecycle Contract

This document is the authoritative contract for how skills are authored, discovered, changed, deleted, and invoked in `odin-os`.

## Canonical source of truth

Skills are authored under:

- `registry/skills/*.md`

Each file is canonical. Runtime code may parse, validate, compile, and reload these files, but it must not create a second authoritative skill registry in SQLite, cache files, or hardcoded lookup tables.

## Required skill contract

Every skill must declare:

- `kind`
- `key`
- `title`
- `summary`
- `version`
- `enabled`
- `strictness`
- `applies_to`
- `scopes`
- `permissions`
- `handler_type`
- `handler_ref`
- `timeout_seconds`
- `input_schema`
- `output_schema`

And every skill must include the required markdown sections from `docs/contracts/registry-format.md`.

Skill keys are part of the on-disk path, so they must use the same lowercase slug shape enforced by the registry validator:

- lowercase letters
- digits
- `-`
- `_`

## Supported execution contract

The initial executable skill contract is intentionally narrow:

- `handler_type` must be `command`
- `handler_ref` must be repo-relative
- `handler_ref` must author under `scripts/skills/`
- `handler_ref` must resolve under `scripts/skills/` after repo-relative and symlink checks
- the handler must be executable
- Odin sends JSON on stdin
- the handler returns JSON on stdout
- Odin enforces `timeout_seconds`
- `permissions` must use the enforced vocabulary below

The first enforced permission vocabulary is:

- `repo.read`
- `runtime.read`
- `repo.mutate.isolated:<action_key>`
- `repo.mutate.full`
- `repo.mutate.governance`
- `repo.mutate.destructive`

Permission validation rules:

- unknown permission strings are rejected before the skill is compiled or invoked
- malformed permission strings are rejected before the skill is compiled or invoked
- `repo.mutate.isolated:<action_key>` requires a non-empty snake_case limited-action key

Permission enforcement rules:

- `repo.read` and `runtime.read` may run in global or project-backed scopes
- mutating permissions are denied in `global` and `new-project` scope
- mutating permissions require resolved project metadata in `project` and `odin-core` scope
- `repo.mutate.isolated:<action_key>` must also pass the selected project's `limited_action` allowlist
- `repo.mutate.full`, `repo.mutate.governance`, and `repo.mutate.destructive` are evaluated through the selected project's transition and approval policy
- system-project mutations inherit the `require_for_system_project_changes` approval gate even if the caller omits it from the request context

Invocation uses one envelope:

- request: `key`, `input`
- response: `skill_key`, `status`, `summary`, `output`, `artifacts`, `raw_ref`, `raw_output`

Command-backed skills execute through the restricted wrapper. The wrapper pins the process cwd to the repo root, strips inherited environment variables down to the allowlisted execution context, and records `execution_profile=restricted_command_v1` on invoke in SQLite.

## CRUD lifecycle

CRUD is owned by `internal/skills.Service` and exposed through `odin skills ...`.

All lifecycle operations coordinate through a repo-scoped lock at:

- `registry/.skill-mutations.lock`

Shared lock:

- `list`
- `get`
- the registry-read portion of `invoke`

Exclusive lock:

- `create`
- `update`
- `delete`

### Create

- validate the skill spec
- render canonical markdown
- write `registry/skills/<key>.md`
- reload the registry and return the normalized skill view

### Read

- list and get read from the compiled registry snapshot produced from `registry/`

### Update

- validate the replacement spec
- rewrite the canonical markdown file
- reload and return the normalized skill view

Updates are rejected before file replacement when validation fails.

### Delete

- reject delete if another registry asset still references the skill
- remove the canonical skill file only when the reference check passes

## Consistency model

Skill CRUD is serialized per repo through the lock file above.

- concurrent writers wait instead of racing on the same registry files
- readers and invocation snapshot resolution wait behind active writers
- `invoke` releases the lock before running the external handler, so the execution uses the skill snapshot resolved at invocation start rather than holding the registry lock for the full command runtime

This is a first hardening step, not a distributed transaction system. It prevents local cross-process file races in the shared repo, but it does not yet introduce semantic version preconditions or persisted rollback records for skill mutations.

## Discovery lifecycle

Skill discovery must use a fresh registry load path instead of a stale startup snapshot.

Broker and planner discovery therefore load through a shared registry-backed source so that:

- newly created skills become visible without process restart
- updated skill metadata is visible on the next catalog read
- deleted skills disappear from the next catalog read

## Codex maintenance workflow

The recommended maintenance workflow for this active Codex session is:

1. inspect available skills with `odin skills list --json`
2. inspect a specific skill with `odin skills get <key> --json`
3. create or update a skill through a spec file:
   - `odin skills create --spec /path/to/skill.json --json`
   - `odin skills update <key> --spec /path/to/skill.json --json`
4. invoke a skill with `odin skills invoke <key> --input '{"field":"value"}' --json`
5. remove a skill with `odin skills delete <key> --json`

Manual file editing is still possible during development, but lifecycle operations should go through `odin skills ...` so validation, rendering, discovery, and invocation stay aligned.

Examples:

- read-only skill in global scope:
  - `odin skills invoke read-only-skill --input '{"message":"hello"}' --json`
- allowlisted isolated mutation in project scope:
  - `odin project select alpha-cli`
  - `odin transition set limited_action allow=docs_audit_note confirm because "skill invoke"`
  - `odin skills invoke isolated-skill --input '{"message":"hello"}' --json`

## Lifecycle observability

`internal/skills.Service` emits structured lifecycle events through an observer hook.

The default CLI path wires a structured log observer, so `odin skills ...` emits audit-friendly JSON log lines to stderr for:

- create
- update
- delete
- invoke
- list
- get

When the service runs through Odin's bootstrapped runtime app, the CLI also appends `skill.lifecycle_recorded` events into the SQLite runtime event stream under `ODIN_ROOT/data/odin.db`.

The package also provides a counter observer so callers can accumulate per-operation success and failure counts from the same event stream without introducing a second source of truth.

Permission-denied invoke events keep the same lifecycle envelope as other failures, but they use stable `error_code` values so operators can distinguish:

- invalid or missing permission declarations
- mutation attempts outside project scope
- transition-state denials
- approval-gated denials

## Drift prevention

To avoid drift:

- do not maintain a second skill list outside `registry/skills`
- do not add per-skill invocation branches in the broker
- do not mutate skill markdown in one code path and runtime state in another
- always reload from the registry after lifecycle changes

## Handler allowlist

Command-backed skill handlers are only valid when the resolved target lives under `scripts/skills/`.

This allowlist is enforced after path cleaning and symlink resolution. A handler may be repo-relative and still be denied if it resolves outside that subtree.
