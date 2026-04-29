---
title: Knowledge Hub Design
status: accepted
date: 2026-04-29
---

# Knowledge Hub Design

## Goal

Add a governed Odin knowledge hub for local Markdown, text, and machine-readable PDF sources such as pilot contracts, books, manuals, and project reference documents.

The v1 goal is not a full library-management UI, graph database, or external RAG platform. It is a real `odin knowledge ...` operator surface that can ingest local files, preserve source provenance, create searchable derived outputs, enforce restricted-source rules, and prove the behavior through the repo-owned Odin command path.

## Domain Source Of Truth

The domain source of truth is `CONTEXT.md`, which defines:

- **Knowledge Source**
- **Knowledge Source Manifest**
- **Knowledge Artifact**
- **Restricted Knowledge Source**
- **Knowledge Operator Surface**
- **Knowledge Source Lifecycle**
- **Restricted Knowledge Use Approval**
- **Supported Knowledge Source Class**
- **OCR-Required Knowledge Artifact**

ADR 0001 remains binding: runtime and index state belong in SQLite, while authored durable memory belongs under `memory/`. The repository layout contract remains binding: `internal/memory` owns knowledge memory access without becoming a second registry.

## Approaches Considered

### 1. Manifest-first local retrieval

Use `memory/knowledge/` manifests, an Odin-managed content-addressed artifact store, SQLite-derived extraction and search state, normalized Markdown snapshots, lightweight graph metadata, and a top-level `odin knowledge` command family.

This is the selected approach because it keeps source authority explicit, works locally, avoids provider cost and privacy issues, and leaves clean extension points for embeddings or richer graph traversal later.

### 2. Markdown library first

Convert every document into Markdown and search those files directly.

This is simple for Codex and tmux sessions, but it weakens source fidelity for contracts and books. Converted Markdown can become mistaken source truth, and restricted-use policy is harder to enforce.

### 3. Graph/RAG platform first

Add embeddings, vector search, a graph database or graph projection, entity extraction, and relationship traversal immediately.

This is too broad for v1. It adds provider choices, cost, privacy, reindexing complexity, and graph modeling before the source/provenance/operator loop is proven.

## Architecture

V1 has five parts.

### `memory/knowledge/*.md`

The reviewed **Knowledge Source Manifest** for each source. It declares source key, title, scope, restricted policy, source class, original path, artifact reference, checksum, refresh policy, extractor expectations, citation policy, topics, entities, relationships, and applicability.

### Odin artifact store

The content-addressed local store for copied source bytes. PDFs, books, contracts, and other large sources stay out of Git. The manifest records original path and checksum, while the artifact store gives Odin stable retrieval even if the original file moves.

### SQLite knowledge tables

SQLite owns derived runtime and index state:

- source lifecycle
- artifact records
- extraction runs
- chunks
- FTS/search rows
- lightweight graph metadata
- normalized Markdown snapshot location
- restricted-use approval events

### `internal/memory/knowledge`

The service boundary for ingest, validate, extract, index, search, refresh, show, inbox handling, and approve-use. It should call store methods and emit runtime events rather than hiding state in files or sidecar scripts.

### `odin knowledge ...`

The canonical operator surface:

- `ingest`
- `inbox`
- `inbox-path`
- `ingest-inbox`
- `list`
- `show`
- `search`
- `refresh`
- `approve-use`

REPL slash commands may alias this surface later, but real E2E proof must use the top-level command.

## Data Flow

### Ingest

1. Operator runs `odin knowledge ingest <path> --scope global` or ingests from the inbox.
2. Odin computes SHA-256, detects source class, and copies bytes into the artifact store.
3. Odin creates or updates `memory/knowledge/<source-key>.md`.
4. SQLite records the Knowledge Source, artifact, lifecycle, and ingest event.
5. The extractor runs for Markdown, text, or machine-readable PDF.
6. Odin writes extracted text, chunks, FTS rows, normalized Markdown snapshot, citation anchors, and lightweight graph metadata.
7. Lifecycle advances to `ready`, or stays `artifact_available`/`failed` with evidence such as `ocr_required`.

### Search

1. Operator runs `odin knowledge search "query"`.
2. Odin resolves scope and restricted policy.
3. SQLite FTS returns matching chunks with source key, title, page or section location, lifecycle, and restricted flag.
4. Output defaults to narrow snippets and citations, especially for Restricted Knowledge Sources.
5. Broader extraction or executor context injection requires a Restricted Knowledge Use Approval.

### Refresh

1. Operator runs `odin knowledge refresh <source-key>` or `--all`.
2. Odin checks artifact checksum, manifest policy, source class, extractor version, and prior lifecycle.
3. If extraction or indexing is stale, Odin rebuilds derived text, Markdown snapshots, chunks, and search rows.
4. Old derived rows remain auditable or superseded, but not authoritative.

### Approve Use

1. Operator records an approval for one source and one use type.
2. Odin records use type, reason, decision, and evidence.
3. Approval does not change source lifecycle.
4. Any downstream consumer may use restricted content only within the approved use.

## Data Model

### SQLite records

- `knowledge_sources`: source key, title, scope, restricted flag, source class, lifecycle, manifest path, current artifact id, current extraction id, created and updated timestamps.
- `knowledge_artifacts`: content hash, size, MIME/source type, artifact path, original path, recorded timestamp.
- `knowledge_extractions`: source id, artifact id, extractor name/version, status, failure code, extracted text hash, normalized Markdown path, started and finished timestamps.
- `knowledge_chunks`: extraction id, source id, chunk text, page/section anchors, ordinal, restricted flag.
- `knowledge_fts`: FTS index over chunk text, title, topics, and entities.
- `knowledge_related_sources`: lightweight graph edges from manifest metadata.
- `restricted_knowledge_use_approvals`: source id, use type, reason, decision, evidence JSON, created timestamp.

### Manifest fields

- `kind: knowledge_source`
- `key`
- `title`
- `scope`
- `restricted`
- `source_kind`
- `source_class`
- `artifact_sha256`
- `artifact_ref`
- `original_path`
- `refresh_policy`
- `extractor`
- `citation_policy`
- `topics`
- `entities`
- `related_sources`
- `applies_to`

## Command Surface

### Direct ingest

```bash
odin knowledge ingest <path> [--scope global] [--key <key>] [--title <title>] [--restricted]
```

Direct ingest copies the file into the artifact store, creates or updates the manifest, extracts supported content, indexes it, and prints lifecycle/provenance output.

### Inbox ingest

Odin exposes a server-side inbox at:

```text
$ODIN_RUNTIME_ROOT/knowledge/inbox/
```

Commands:

```bash
odin knowledge inbox [--json]
odin knowledge inbox-path
odin knowledge ingest-inbox <name> [--scope global] [--restricted] [--title <title>]
odin knowledge ingest-inbox --all [--scope global] [--restricted]
```

The operator can copy files to the inbox with `scp`, `rsync`, SFTP, or a mounted folder, then ingest explicitly through Odin. Imported files move to `knowledge/imported/`; rejected or unsupported files remain visible under a rejected path with a clear reason.

### Inspection and search

```bash
odin knowledge list [--scope <scope>] [--restricted] [--json]
odin knowledge show <source-key> [--json]
odin knowledge search <query> [--scope <scope>] [--limit <n>] [--json]
odin knowledge refresh <source-key|--all> [--json]
```

Search defaults to ready sources and narrow snippets. Restricted sources include citations but do not expose broad content unless a separate approval exists.

### Restricted use approval

```bash
odin knowledge approve-use <source-key> --use <type> --reason <text>
```

V1 use types:

- `bulk_export`
- `broad_extraction`
- `sharing`
- `executor_context_injection`

Approvals are single-use by default. Each approval records one source, one use type, one reason, one operator decision, and one evidence record. Exact flag spelling may change during implementation, but the single-use/use-type scoped model is locked.

## Derived Markdown Snapshots

V1 generates a normalized Markdown snapshot for each successfully extracted source.

These snapshots are rebuildable derived output, not source truth. They exist to make Odin, Codex tmux sessions, skills, and human inspection easier. Each snapshot must identify the source key, artifact checksum, extraction id, extractor name/version, and source locations when available.

Restricted-source snapshots must live in a protected derived-output path and must not be treated as public docs.

## Lightweight Graph Metadata

V1 includes a lightweight knowledge graph only as manifest and index metadata. There is no graph database in v1.

Manifest/index metadata may include:

- `topics`
- `source_kind`
- `related_sources`
- `entities`
- `applies_to`

SQLite can index these fields for filters and related-source lookup. Advanced entity extraction, graph traversal, and graph databases are deferred until after ingest, citation, and search are proven.

## Error Handling

- Unsupported file type: lifecycle `failed`, failure code `unsupported_source_class`.
- Scanned/image PDF: lifecycle `artifact_available` or `failed`, failure code `ocr_required`.
- Missing artifact: lifecycle `failed`, failure code `artifact_missing`.
- Invalid manifest: command fails before extraction or indexing.
- Checksum mismatch: lifecycle `stale` or `failed`, depending on recoverability.
- Extractor crash: lifecycle `failed`, with extractor name/version and error summary.
- Search over non-ready source: excluded by default unless an implementation adds an explicit include flag.

Odin must fail visibly with source and lifecycle evidence. It must not silently index low-confidence content.

## Safety Rules

- Personal reference documents such as pilot contracts, books, and manuals default to global scope and restricted policy.
- Project-specific sources require an explicit managed-project scope in the manifest.
- Restricted sources return narrow cited snippets by default.
- Bulk export, broad extraction, sharing, and executor-context injection require single-use approval.
- `--json` output still respects restricted snippet limits unless approval is attached to a downstream use.
- OCR is out of scope for v1 and must not silently advance a source to `ready`.

## Testing Strategy

Testing should prove the real operator path, not just helper functions.

Required tests:

- Store tests for sources, artifacts, lifecycle, chunks, relationships, and approvals.
- Service tests for ingest, refresh, search, restricted approvals, inbox handling, and failure codes.
- CLI tests for `knowledge ingest`, `inbox`, `inbox-path`, `ingest-inbox`, `list`, `show`, `search`, `refresh`, and `approve-use`.
- Fixture tests with Markdown, plain text, machine-readable PDF, unsupported file, and OCR-required PDF when a fixture is available.

Required real Odin smoke:

1. Build `./bin/odin`.
2. Use a temp `ODIN_ROOT`.
3. Copy a fixture into the inbox.
4. Run `odin knowledge inbox`.
5. Run `odin knowledge ingest-inbox`.
6. Run `odin knowledge list`.
7. Run `odin knowledge show`.
8. Run `odin knowledge search`.
9. Verify restricted snippet behavior.
10. Run `odin knowledge approve-use`.
11. Verify OCR-required fixtures do not become `ready`.

## Non-Goals

- OCR support for scanned or image-only PDFs.
- Embeddings or vector search.
- Graph database or advanced graph traversal.
- Library-management UI.
- Annotation workflows.
- Treating converted Markdown as canonical source truth.
- Direct Codex context dumps.
- Sidecar RAG scripts outside the Odin operator surface.

## Current Implementation Gap

The current binary does not implement `odin knowledge`; it returns `unknown command: knowledge`. Implementation work must add that canonical command family and prove it through real `odin knowledge ...` commands.
