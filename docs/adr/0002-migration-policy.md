---
title: ADR 0002 - Migration Policy
status: accepted
date: 2026-04-08
phase: "00"
source_repo: odin-orchestrator
---

# ADR 0002: Migration Policy

## Context

The legacy `odin-orchestrator` repository contains prompts, scripts, operational knowledge, and implementation ideas, but it is not the runtime root for the new system. Migration must be explicit, reviewable, and biased toward normalization into the new contracts rather than cargo-cult reuse.

## Decision

Every legacy asset must be classified into exactly one migration action before it is copied, rewritten, or discarded:

- `migrate_as_is`
- `rewrite`
- `reference_only`
- `archive`
- `delete`

No large legacy directory may be copied wholesale into this repository.

## Migration action definitions

### `migrate_as_is`

Use only when the asset already matches the new contract closely enough that copying it preserves clarity.

Criteria:

- already in Markdown with frontmatter, or trivially normalized
- no hidden runtime dependency on the old repository structure
- no incompatible ownership model
- no permanent compatibility shim required

### `rewrite`

Use when the underlying idea is useful but the asset must be reshaped into the new Go-first, SQLite-first, contract-driven model.

Typical candidates:

- prompts that need new system assumptions
- registry-like documents that need frontmatter normalization
- scripts that encode useful workflows but assume the old repo layout
- code in non-canonical languages that expresses behavior worth preserving

### `reference_only`

Use when the asset is valuable as research or migration context, but should not become part of the runtime or canonical authored set.

Typical candidates:

- legacy ADRs and design notes
- operational runbooks that inform new docs
- implementation experiments whose ideas matter more than the code

### `archive`

Use when the asset should be retained for audit or historical reasons but should not be used as an active source.

Typical candidates:

- superseded migrations
- one-off support material tied to the old repo
- compatibility shims awaiting removal

### `delete`

Use when the asset adds noise, duplicates newer authority, or has no justified future role.

Typical candidates:

- generated artifacts
- stale caches, logs, and temporary outputs
- dead experiments with no continuing value

## Default migration bias by asset type

| Legacy asset type | Default action | Notes |
| --- | --- | --- |
| Agents, skills, workflows, commands in ad hoc formats | `rewrite` | Normalize into Markdown with frontmatter under the new registry contracts |
| Markdown docs that already describe durable policy clearly | `migrate_as_is` or `reference_only` | Promote only if they still fit the new authority model |
| Runtime code outside the new Go-first boundary | `reference_only` or `rewrite` | Behavior may migrate; implementation should not be copied blindly |
| Scripts with enduring operational value | `rewrite` | Keep only if they support the new repo shape |
| Generated files, logs, caches, snapshots | `delete` | Never migrate as runtime authority |
| Legacy compatibility layers | `archive` or `delete` | No compatibility layer is permanent unless explicitly promoted |

## Migration workflow

For each asset or asset group:

1. Inventory the source and its purpose.
2. Assign one migration action.
3. Record why that action is justified.
4. If the action is `rewrite`, convert the behavior or knowledge into the new contract rather than mirroring folder shape.
5. Add migration notes in the receiving phase when the old concept is replaced, split, or removed.
6. Delete or archive leftovers intentionally; do not leave ambiguous duplicates.

## Guardrails

- The new repository is the canonical future.
- `odin-orchestrator` is a reference input, not an execution dependency.
- Legacy naming, folder structure, or language choice does not create automatic entitlement in the new repo.
- Permanent ambiguity between old and new authorities is a migration failure.

## Non-goals

This policy does not attempt to preserve one-to-one folder parity with the old repository. It also does not authorize temporary compatibility layers to become de facto permanent architecture.
