---
title: Registry Format Contract
status: active
date: 2026-04-16
phase: "02"
---

# Registry Format Contract

This document defines the canonical authored format for registry assets under `registry/` and the normalized `odin/v1` manifest contract used by the compiler.

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
- path kind and frontmatter `kind` differ
- a required normalized field is missing from an `odin/v1` manifest
- a required Markdown section is missing or empty
- multiple files declare the same `key`

Validation failures must be returned as diagnostics. They must not crash the daemon or abort compilation of unrelated valid files.

## Compilation behavior

Compilation produces normalized in-memory objects and diagnostics. Invalid files are excluded from the compiled item set. Valid files remain loadable even when sibling files are invalid.

## Hot reload compatibility

The load pipeline must be snapshot-oriented and deterministic so a future watcher can re-run the same scanner, parser, validator, and compiler path after filesystem changes.
