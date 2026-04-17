---
title: Registry Format Contract
status: active
date: 2026-04-16
phase: "02"
---

# Registry Format Contract

This document defines the canonical authored format for registry assets under `registry/` and the normalized `odin/v1` manifest contract used by the compiler.

Skill-specific execution and lifecycle rules are defined in `docs/contracts/skill-lifecycle.md`. This document only defines the authored registry shape and validation rules.

## Supported kinds

Registry kinds are:

- `agent`
- `skill`
- `workflow`
- `command`

Canonical locations are:

- `registry/agents/*.md`
- `registry/skills/*.md`
- `registry/workflows/*.md`
- `registry/commands/*.md`

The directory kind and frontmatter `kind` must agree.

## Authored format

Markdown files remain the authored source of truth. Files must begin with YAML frontmatter delimited by `---` and include the required Markdown sections:

### Legacy authored fields

Legacy markdown manifests remain supported during the migration. Common required legacy fields are:

- `kind`
- `key`
- `title`
- `summary`

Common optional legacy fields are:

- `status`
- `tags`
- `owners`

Kind-specific legacy requirements:

- `agent`: `role`, `scopes`, `tools`
- `skill`: `version`, `enabled`, `strictness`, `applies_to`, `scopes`, `permissions`, `handler_type`, `handler_ref`, `timeout_seconds`, `input_schema`, `output_schema`
- `workflow`: `entrypoint`, `composes`
- `command`: `command`, `scopes`

Legacy command manifests may also declare `aliases`.

## Required Markdown sections

Each registry file must include these level-two headings:

- `## Purpose`
- `## When to Use`
- `## Inputs`
- `## Procedure`
- `## Outputs`
- `## Constraints`
- `## Success Criteria`

Section bodies must be non-empty after trimming whitespace.

## Normalized manifest contract

When `apiVersion: odin/v1` is present, the manifest is treated as normalized and must include:

- `apiVersion`
- `kind`
- `name`
- `version`
- `availability`
- `permissions`
- `inputSchema`
- `outputSchema`
- `dependencies`
- `execution`
- `implementation`

Invokable kinds must provide both `inputSchema` and `outputSchema`. Versioned manifests must not omit `version`.

### Normalized frontmatter fields

- `apiVersion`
- `kind`
- `name`
- `version`
- `availability.scope`
- `availability.mode`
- `permissions`
- `inputSchema.ref`
- `inputSchema.type`
- `outputSchema.ref`
- `outputSchema.type`
- `dependencies`
- `execution.mode`
- `execution.timeout`
- `implementation.kind`
- `implementation.ref`
- `implementation.path`

## Legacy compatibility

The loader and compiler continue to accept existing legacy manifests during the staged migration. Legacy frontmatter fields such as `key`, `title`, `summary`, `strictness`, `applies_to`, `entrypoint`, `composes`, and `command` remain supported for now.

Legacy files are compiled into normalized in-memory items where possible, but they are not required to declare `odin/v1` until the migration cutover lands.

## Validation rules

The registry compiler must reject a file clearly when:

- frontmatter is missing
- frontmatter YAML is invalid
- `kind` is unknown
- `key` is not a lowercase slug using only letters, digits, `-`, or `_`
- path kind and frontmatter `kind` differ
- a required normalized field is missing from an `odin/v1` manifest
- a required legacy field is missing from a legacy manifest
- a skill `handler_ref` leaves the repo or points outside `scripts/skills/`
- a required Markdown section is missing or empty
- multiple files declare the same `key`

Validation failures must be returned as diagnostics. They must not crash the daemon or abort compilation of unrelated valid files.

## Compilation behavior

Compilation produces normalized in-memory objects and diagnostics. Invalid files are excluded from the compiled item set. Valid files remain loadable even when sibling files are invalid.

## Hot reload compatibility

The load pipeline must be snapshot-oriented and deterministic so a future watcher can re-run the same scanner, parser, validator, and compiler path after filesystem changes.
