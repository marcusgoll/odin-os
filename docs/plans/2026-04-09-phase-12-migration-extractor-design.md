# Phase 12 Migration Extractor Design

## Goal

Build a deterministic migration extractor that scans `odin-orchestrator`, produces a normalized inventory of useful candidate assets and runtime concepts, detects likely duplicates and backup trees, and optionally emits draft registry assets for human review.

## Current Context

The old `odin-orchestrator` repository clearly contains valuable material, but it also contains large amounts of overlap and historical debris:

- multiple skill directories under both `.claude/skills` and `.agents/skills`
- backup-style trees such as `agents-skills-backup`
- repo-local `.worktrees`
- cache and git metadata that must never be treated as canonical assets
- architecture and operational docs mixed with generated or stale files

Phase 12 should make migration deliberate. It should not copy large legacy directories or silently promote old artifacts into the new runtime.

## Approaches Considered

### 1. Script-only scanner

This would walk the old repo and dump filenames with light tagging. It is fast to write, but it would not be reliable enough for deliberate migration review.

### 2. Typed extractor with classification, duplicate detection, and optional drafts

This keeps extraction deterministic and reviewable. It produces machine-readable inventory, human-readable reports, and draft registry files only when requested. This is the recommended approach.

### 3. Auto-promoting migrator

This would write directly into `registry/` or `prompts/`. It is too risky for this phase because it would blur extraction and promotion.

## Recommendation

Implement a typed migration extractor in Go under `internal/migration/extractor`, with committed review artifacts under `docs/migration/` and `state/migration/`. Draft normalized registry files should be opt-in and must be written under `state/migration/drafts/`, never directly into canonical registry folders.

## Output Model

Phase 12 should always produce:

- `docs/migration/legacy-inventory.md`
- `docs/migration/duplicate-report.md`
- `state/migration/inventory.json`

When draft generation is enabled, it should also produce:

- `state/migration/drafts/<kind>/<key>.md`

Draft files must be clearly marked as drafts and must conform to the current Phase 02 registry contract.

## Candidate Kinds

The extractor should scan the old repo for these candidate kinds:

- `skill`
- `agent`
- `workflow`
- `prompt`
- `architecture_doc`
- `operational_playbook`

Candidate kind detection should be deterministic and path-driven first, with light content inspection only when necessary.

## Classification Model

Every candidate should be classified as one of:

- `migrate_as_is`
- `rewrite`
- `reference_only`
- `archive`
- `delete`

These classifications should reuse the migration authority already defined in the repo, but apply them at the asset level.

## Heuristics

### Ignore rules

The extractor must ignore obvious non-canonical trees such as:

- `.git`
- `.cache`
- `.worktrees`
- vendored module caches
- temporary files
- backup or worktree internals that are not candidate assets

### Duplicate and backup detection

The extractor should detect likely duplicates using:

- exact content hash matches
- repeated normalized keys or titles
- path signals such as `backup`, `copy`, `old`, `.worktrees`, or `tmp`
- known mirrored roots like `.claude/skills` and `.agents/skills`

### Classification defaults

- `delete` for generated, cached, or clearly non-canonical files
- `archive` for backups and stale shadow trees
- `reference_only` for architecture docs and migration notes that inform design but should not become runtime assets directly
- `rewrite` for skills, workflows, prompts, and playbooks that contain useful content but do not already match the new contract
- `migrate_as_is` only for assets that already map cleanly to the new repo contract without major reshaping

## Normalized Metadata

Each inventory record should include:

- source path
- relative path
- detected candidate kind
- normalized key
- extracted title
- content hash
- path signals
- duplicate group id when applicable
- classification
- rationale

This metadata should be stable enough for deterministic reruns.

## Draft Generation

Draft generation should be optional and conservative.

Rules:

- drafts are emitted only for selected asset kinds that map to the new registry model
- drafts are emitted only for primary candidates, not every duplicate
- drafts must include frontmatter with `status: draft`
- drafts must preserve provenance, for example by recording the legacy source path in the body
- drafts should use generated placeholder sections when the old asset does not map cleanly

Phase 12 should not attempt to auto-promote prompts or architecture docs into runtime registry assets unless they clearly map to a supported kind.

## Implementation Shape

Recommended structure:

- `internal/migration/extractor/types.go`
- `internal/migration/extractor/scan.go`
- `internal/migration/extractor/classify.go`
- `internal/migration/extractor/duplicates.go`
- `internal/migration/extractor/drafts.go`
- `internal/migration/extractor/reports.go`
- `internal/migration/extractor/service.go`

For a runnable entry point, Phase 12 can add a small Go-based migration script under `scripts/migrate/` that invokes the extractor against a source root and writes the outputs.

## Testing Strategy

Tests should cover:

1. useful assets are distinguished from junk and backup trees
2. duplicate detection groups mirrored or copied assets correctly
3. classification is deterministic for known patterns
4. generated draft registry files conform to the current registry contract
5. generated reports are stable enough to compare in tests

Use temp fixture directories in tests rather than relying only on the real legacy repo.

## Non-Goals

Phase 12 does not include:

- automatic promotion into canonical runtime folders
- blind folder copying
- preserving old folder topology as runtime structure
- treating backup directories as canonical
- runtime execution of migrated assets

## Success Criteria

Phase 12 succeeds when migration review becomes deliberate: useful assets are visible, junk is distinguishable, duplicates are called out explicitly, and optional draft assets can be reviewed against the new contract before promotion.
