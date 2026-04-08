# Phase 02 Markdown Registry Design

**Date:** 2026-04-08

## Goal

Define a Markdown-frontmatter registry contract for agents, skills, workflows, and commands that stays human-readable in Git while compiling safely into normalized runtime objects.

## Recommended Approach

Use a hybrid parser:

- `yaml.v3` for frontmatter decoding
- a constrained heading-based Markdown section extractor for required sections
- a pure snapshot compiler pipeline that can later sit behind hot-reload without changing the authoring contract

This keeps Phase 02 reviewable and deterministic without introducing a full Markdown AST dependency or inventing a fragile custom YAML parser.

## Authoring Contract

Registry files live under:

- `registry/agents/*.md`
- `registry/skills/*.md`
- `registry/workflows/*.md`
- `registry/commands/*.md`

Every registry file must:

1. start with YAML frontmatter delimited by `---`
2. include a `kind` field that matches both the validator and its directory
3. include the required Markdown sections:
   - `Purpose`
   - `When to Use`
   - `Inputs`
   - `Procedure`
   - `Outputs`
   - `Constraints`
   - `Success Criteria`

## Frontmatter Shape

Common required fields:

- `kind`
- `key`
- `title`
- `summary`

Common optional fields:

- `status`
- `tags`
- `owners`

Kind-specific required fields:

- `agent`: `role`, `scopes`, `tools`
- `skill`: `strictness`, `applies_to`
- `workflow`: `entrypoint`, `composes`
- `command`: `command`, `scopes`

Kind-specific optional fields:

- `command`: `aliases`

## Runtime Pipeline

### Scanner

The scanner walks `registry/` recursively, accepts only `.md` files, infers the expected kind from the first directory segment below the root, and returns stable path-ordered file descriptors.

### Parser

The parser splits frontmatter from body, decodes YAML into a generic frontmatter struct, extracts `##` sections, and preserves raw content needed for diagnostics and future compilation metadata.

### Validator

The validator checks:

- file path kind matches frontmatter kind
- required common frontmatter exists
- required kind-specific frontmatter exists
- all required Markdown sections exist and are non-empty
- duplicate `key` values are rejected clearly

Validation returns diagnostics and never panics or aborts the process.

### Compiler

The compiler turns valid parsed documents into normalized registry items and returns a snapshot containing:

- compiled items
- per-file diagnostics
- valid item indexes by kind and key

Invalid files are excluded from the compiled item set but included in diagnostics.

### Hot Reload Readiness

Phase 02 will not implement live watching behavior, but it will expose a watcher contract and a no-op watcher so later phases can add file watching without changing loader or compiler contracts.

## Package Shape

- `internal/registry` for shared types and diagnostics
- `internal/registry/parser` for frontmatter and section extraction
- `internal/registry/validator` for schema and semantic validation
- `internal/registry/compiler` for normalized snapshot compilation
- `internal/registry/loader` for scanning and end-to-end load
- `internal/registry/watcher` for the future hot-reload contract

## Testing Strategy

Add tests for:

- valid registry file parse and compile
- missing frontmatter
- invalid `kind`
- path kind mismatch
- missing required section
- missing kind-specific field
- duplicate keys across files
- mixed valid and invalid files producing a partial snapshot with diagnostics

## Non-Goals

- live file watching
- Markdown AST fidelity beyond required heading sections
- registry persistence in SQLite
- migration of legacy `odin-orchestrator` assets
