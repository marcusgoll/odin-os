---
title: Registry Format Contract
status: active
date: 2026-04-08
phase: "02"
---

# Registry Format Contract

This document defines the canonical authored format for registry assets under `registry/`. Markdown with frontmatter is the authored truth. Runtime code may compile and index these files, but it must not replace them as the source of truth.

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

## Required frontmatter

Every registry file must begin with YAML frontmatter delimited by `---`.

### Common required fields

- `kind`
- `key`
- `title`
- `summary`

### Common optional fields

- `status`
- `tags`
- `owners`

### Kind-specific required fields

#### Agent

- `role`
- `scopes`
- `tools`

#### Skill

- `strictness`
- `applies_to`

#### Workflow

- `entrypoint`
- `composes`

#### Command

- `command`
- `scopes`

### Kind-specific optional fields

#### Command

- `aliases`

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

## Validation rules

The registry compiler must reject a file clearly when:

- frontmatter is missing
- frontmatter YAML is invalid
- `kind` is unknown
- path kind and frontmatter `kind` differ
- a required common field is missing
- a required kind-specific field is missing
- a required Markdown section is missing or empty
- multiple files declare the same `key`

Validation failures must be returned as diagnostics. They must not crash the daemon or abort compilation of unrelated valid files.

## Compilation behavior

Compilation produces normalized in-memory objects and diagnostics. Invalid files are excluded from the compiled item set. Valid files remain loadable even when sibling files are invalid.

## Hot reload compatibility

The load pipeline must be snapshot-oriented and deterministic so a future watcher can re-run the same scanner, parser, validator, and compiler path after filesystem changes.
