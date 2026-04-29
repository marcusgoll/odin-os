# Knowledge Hub Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the v1 `odin knowledge ...` surface for manifest-first local retrieval over Markdown, text, and machine-readable PDF sources.

**Domain Source of Truth:** `CONTEXT.md`, `docs/plans/2026-04-29-knowledge-hub-design.md`, `docs/adr/0001-canonical-authority.md`, `docs/contracts/repo-layout.md`, `docs/contracts/verification-model.md`

**Context:** Odin OS knowledge memory and retrieval

**Owns / Does Not Own:** Odin owns Knowledge Sources, Knowledge Source Manifests, Knowledge Artifacts, Knowledge Source Lifecycle, Restricted Knowledge Use Approvals, derived retrieval artifacts, and the `odin knowledge ...` operator surface. It does not own FLICA/PBS domain truth, registry workflow definitions, OCR, vector search, graph databases, or raw Codex context dumping.

**Invariants:**
- Every Knowledge Source must have a manifest before it is an approved retrieval input.
- Personal reference documents default to global restricted scope unless explicitly bound to a managed project.
- Original artifact plus manifest are authoritative; extracted text, chunks, Markdown snapshots, FTS rows, and graph metadata are rebuildable projections.
- V1 extraction supports only Markdown, plain text, and machine-readable PDFs.
- OCR-required artifacts must not silently advance to `extracted`, `indexed`, or `ready`.
- Restricted broader use requires a single-use, use-type scoped approval event.
- Real E2E proof for user-visible knowledge behavior must exercise `odin knowledge ...`.

**Architecture:** Add a dedicated `internal/memory/knowledge` service over new SQLite knowledge tables and an Odin-managed content-addressed artifact store. Expose the service through a top-level `odin knowledge ...` command family wired from `internal/app/lifecycle`, with any future REPL alias treated as a thin adapter. Use SQLite FTS for v1 lexical search, manifest metadata for lightweight graph relationships, and generated Markdown snapshots as protected derived output.

**Tech Stack:** Go, SQLite/FTS5 through `modernc.org/sqlite`, `gopkg.in/yaml.v3`, `github.com/ledongthuc/pdf` for machine-readable PDF text extraction, existing Odin lifecycle/bootstrap/store/test helpers

---

### Task 1: Add Knowledge Source storage schema and events

**Domain Goal:** Persist Knowledge Sources, Knowledge Artifacts, lifecycle state, extraction runs, chunks, lightweight graph edges, and Restricted Knowledge Use Approvals under SQLite runtime authority.

**Domain Rules Enforced:**
- Runtime/index state belongs in SQLite.
- Artifact plus manifest are authoritative; derived rows reference source and extraction ids.
- Approval state is separate from Knowledge Source Lifecycle.

**Why this matters:**
- Without a durable store, `odin knowledge ...` would either become a sidecar script or hide retrieval state in generated files.

**Files:**
- Create: `internal/store/sqlite/migrations/0019_knowledge_sources.sql`
- Modify: `internal/store/sqlite/models.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`
- Modify: `internal/runtime/events/events.go`

**Step 1: Write failing migration/store tests**

Add tests to `internal/store/sqlite/store_test.go`:

```go
func TestStorePersistsKnowledgeSourceArtifactExtractionAndChunks(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "knowledge-source.db")
	defer store.Close()

	artifact, err := store.RecordKnowledgeArtifact(ctx, sqlite.RecordKnowledgeArtifactParams{
		SHA256:       "sha256:abc",
		SizeBytes:    42,
		SourceType:   "text",
		MimeType:     "text/plain",
		ArtifactPath: "knowledge/artifacts/ab/abc/source.txt",
		OriginalPath: "/tmp/source.txt",
	})
	if err != nil {
		t.Fatalf("RecordKnowledgeArtifact() error = %v", err)
	}

	source, err := store.UpsertKnowledgeSource(ctx, sqlite.UpsertKnowledgeSourceParams{
		Key:               "pilot-contract",
		Title:             "Pilot Contract",
		Scope:             "global",
		ScopeKey:          "global",
		Restricted:        true,
		SourceKind:        "pilot_contract",
		SourceClass:       "text",
		Lifecycle:         "artifact_available",
		ManifestPath:      "memory/knowledge/pilot-contract.md",
		CurrentArtifactID: &artifact.ID,
	})
	if err != nil {
		t.Fatalf("UpsertKnowledgeSource() error = %v", err)
	}

	extraction, err := store.RecordKnowledgeExtraction(ctx, sqlite.RecordKnowledgeExtractionParams{
		SourceID:               source.ID,
		ArtifactID:             artifact.ID,
		ExtractorName:          "plain_text",
		ExtractorVersion:       "v1",
		Status:                 "succeeded",
		ExtractedTextHash:      "sha256:text",
		NormalizedMarkdownPath: "state/knowledge/normalized/pilot-contract.md",
	})
	if err != nil {
		t.Fatalf("RecordKnowledgeExtraction() error = %v", err)
	}

	chunk, err := store.RecordKnowledgeChunk(ctx, sqlite.RecordKnowledgeChunkParams{
		SourceID:     source.ID,
		ExtractionID: extraction.ID,
		Ordinal:      1,
		Text:         "Vacation accrual section.",
		Anchor:       "section:vacation",
		Restricted:   true,
	})
	if err != nil {
		t.Fatalf("RecordKnowledgeChunk() error = %v", err)
	}

	results, err := store.SearchKnowledgeChunks(ctx, sqlite.SearchKnowledgeChunksParams{
		Query: "vacation",
		Scope: "global",
		ScopeKey: "global",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchKnowledgeChunks() error = %v", err)
	}
	if len(results) != 1 || results[0].ChunkID != chunk.ID {
		t.Fatalf("results = %+v, want chunk %d", results, chunk.ID)
	}
}
```

Add approval separation test:

```go
func TestStoreRecordsRestrictedKnowledgeUseApprovalWithoutChangingLifecycle(t *testing.T) {
	// create source with Lifecycle "ready"
	// call RecordRestrictedKnowledgeUseApproval(... UseType: "executor_context_injection")
	// reload source and assert Lifecycle is still "ready"
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/sqlite -run 'TestStore.*Knowledge' -count=1 -v`

Expected: FAIL because knowledge tables and store methods do not exist.

**Step 3: Add migration**

Create `0019_knowledge_sources.sql` with:

```sql
CREATE TABLE IF NOT EXISTS knowledge_artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  sha256 TEXT NOT NULL UNIQUE,
  size_bytes INTEGER NOT NULL,
  source_type TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  artifact_path TEXT NOT NULL,
  original_path TEXT NOT NULL,
  recorded_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS knowledge_sources (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  key TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  scope TEXT NOT NULL,
  scope_key TEXT NOT NULL,
  restricted INTEGER NOT NULL,
  source_kind TEXT NOT NULL,
  source_class TEXT NOT NULL,
  lifecycle TEXT NOT NULL,
  manifest_path TEXT NOT NULL,
  current_artifact_id INTEGER REFERENCES knowledge_artifacts(id) ON DELETE SET NULL,
  current_extraction_id INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_knowledge_sources_scope ON knowledge_sources(scope, scope_key, key);
CREATE INDEX IF NOT EXISTS idx_knowledge_sources_lifecycle ON knowledge_sources(lifecycle, key);

CREATE TABLE IF NOT EXISTS knowledge_extractions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
  artifact_id INTEGER NOT NULL REFERENCES knowledge_artifacts(id) ON DELETE CASCADE,
  extractor_name TEXT NOT NULL,
  extractor_version TEXT NOT NULL,
  status TEXT NOT NULL,
  failure_code TEXT NOT NULL DEFAULT '',
  failure_summary TEXT NOT NULL DEFAULT '',
  extracted_text_hash TEXT NOT NULL DEFAULT '',
  normalized_markdown_path TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  finished_at TEXT
);

CREATE TABLE IF NOT EXISTS knowledge_chunks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
  extraction_id INTEGER NOT NULL REFERENCES knowledge_extractions(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  text TEXT NOT NULL,
  anchor TEXT NOT NULL DEFAULT '',
  page_number INTEGER,
  restricted INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(extraction_id, ordinal)
);

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(
  source_key,
  title,
  topics,
  entities,
  chunk_text,
  content='',
  tokenize='unicode61'
);

CREATE TABLE IF NOT EXISTS knowledge_related_sources (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
  related_source_key TEXT NOT NULL,
  relationship TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(source_id, related_source_key, relationship)
);

CREATE TABLE IF NOT EXISTS restricted_knowledge_use_approvals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
  use_type TEXT NOT NULL,
  reason TEXT NOT NULL,
  decision TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  decided_by TEXT NOT NULL,
  decided_at TEXT NOT NULL
);
```

**Step 4: Add models and store methods**

Add model structs and params in `models.go`:

```go
type KnowledgeSource struct { /* fields from table */ }
type KnowledgeArtifact struct { /* fields from table */ }
type KnowledgeExtraction struct { /* fields from table */ }
type KnowledgeChunk struct { /* fields from table */ }
type KnowledgeSearchResult struct { SourceKey, Title, Text, Anchor string; PageNumber *int64; Restricted bool; ChunkID int64 }
type RestrictedKnowledgeUseApproval struct { /* fields from table */ }
```

Add store methods in `store.go`:

```go
func (store *Store) RecordKnowledgeArtifact(ctx context.Context, params RecordKnowledgeArtifactParams) (KnowledgeArtifact, error)
func (store *Store) UpsertKnowledgeSource(ctx context.Context, params UpsertKnowledgeSourceParams) (KnowledgeSource, error)
func (store *Store) GetKnowledgeSourceByKey(ctx context.Context, key string) (KnowledgeSource, error)
func (store *Store) ListKnowledgeSources(ctx context.Context, params ListKnowledgeSourcesParams) ([]KnowledgeSource, error)
func (store *Store) RecordKnowledgeExtraction(ctx context.Context, params RecordKnowledgeExtractionParams) (KnowledgeExtraction, error)
func (store *Store) RecordKnowledgeChunk(ctx context.Context, params RecordKnowledgeChunkParams) (KnowledgeChunk, error)
func (store *Store) IndexKnowledgeChunk(ctx context.Context, params IndexKnowledgeChunkParams) error
func (store *Store) SearchKnowledgeChunks(ctx context.Context, params SearchKnowledgeChunksParams) ([]KnowledgeSearchResult, error)
func (store *Store) RecordRestrictedKnowledgeUseApproval(ctx context.Context, params RecordRestrictedKnowledgeUseApprovalParams) (RestrictedKnowledgeUseApproval, error)
```

**Step 5: Add runtime events**

Add event types and payloads in `internal/runtime/events/events.go`:

```go
EventKnowledgeSourceIngested Type = "knowledge.source_ingested"
EventKnowledgeSourceLifecycleChanged Type = "knowledge.lifecycle_changed"
EventKnowledgeExtractionRecorded Type = "knowledge.extraction_recorded"
EventRestrictedKnowledgeUseApproved Type = "knowledge.restricted_use_approved"
```

**Step 6: Run tests**

Run: `go test ./internal/store/sqlite ./internal/runtime/events -run 'TestStore.*Knowledge|Test.*Knowledge' -count=1 -v`

Expected: PASS.

**Step 7: Commit**

```bash
git add internal/store/sqlite internal/runtime/events/events.go
git commit -m "feat(store): add knowledge source persistence"
```

### Task 2: Add Knowledge Artifact and extraction service for Markdown and text

**Domain Goal:** Implement manifest-first ingestion for local Markdown and text without adding sidecar scripts or direct manifest-edit workflows.

**Domain Rules Enforced:**
- Each source has a manifest before approved retrieval.
- Artifacts are content-addressed and kept out of Git.
- Normalized Markdown snapshots are derived output, not source truth.

**Why this matters:**
- This proves the core loop before PDF extraction and command wiring.

**Files:**
- Create: `internal/memory/knowledge/types.go`
- Create: `internal/memory/knowledge/artifacts.go`
- Create: `internal/memory/knowledge/manifest.go`
- Create: `internal/memory/knowledge/extractors.go`
- Create: `internal/memory/knowledge/service.go`
- Create: `internal/memory/knowledge/service_test.go`
- Create: `internal/memory/knowledge/testdata/pilot-contract.txt`
- Create: `internal/memory/knowledge/testdata/manual.md`

**Step 1: Write failing service tests**

Add tests:

```go
func TestServiceIngestsTextAsGlobalRestrictedKnowledgeSource(t *testing.T)
func TestServiceIngestsMarkdownAndWritesNormalizedSnapshot(t *testing.T)
func TestServiceRejectsUnsupportedSourceClass(t *testing.T)
```

Example assertion:

```go
result, err := service.Ingest(ctx, knowledge.IngestParams{
	Path:       fixturePath,
	Key:        "pilot-contract",
	Title:      "Pilot Contract",
	Scope:      "global",
	ScopeKey:   "global",
	Restricted: true,
})
if err != nil { t.Fatalf("Ingest() error = %v", err) }
if result.Source.Lifecycle != knowledge.LifecycleReady {
	t.Fatalf("Lifecycle = %q, want ready", result.Source.Lifecycle)
}
if !strings.HasPrefix(result.Artifact.SHA256, "sha256:") {
	t.Fatalf("artifact hash = %q", result.Artifact.SHA256)
}
if _, err := os.Stat(filepath.Join(repoRoot, "memory", "knowledge", "pilot-contract.md")); err != nil {
	t.Fatalf("manifest missing: %v", err)
}
if _, err := os.Stat(result.NormalizedMarkdownPath); err != nil {
	t.Fatalf("normalized markdown missing: %v", err)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/knowledge -run TestServiceIngests -count=1 -v`

Expected: FAIL because the package has no implementation.

**Step 3: Implement domain types**

In `types.go` define constants matching `CONTEXT.md`:

```go
type Lifecycle string
const (
	LifecycleDeclared Lifecycle = "declared"
	LifecycleArtifactAvailable Lifecycle = "artifact_available"
	LifecycleExtracted Lifecycle = "extracted"
	LifecycleIndexed Lifecycle = "indexed"
	LifecycleReady Lifecycle = "ready"
	LifecycleStale Lifecycle = "stale"
	LifecycleFailed Lifecycle = "failed"
)

type SourceClass string
const (
	SourceClassMarkdown SourceClass = "markdown"
	SourceClassText SourceClass = "text"
	SourceClassMachineReadablePDF SourceClass = "machine_readable_pdf"
	SourceClassOCRRequired SourceClass = "ocr_required"
)
```

**Step 4: Implement artifact store**

`artifacts.go` should copy bytes to:

```text
$ODIN_RUNTIME_ROOT/knowledge/artifacts/<first-two-sha>/<sha>/<filename>
```

It must return SHA-256, size, artifact path, and original path. Re-ingesting identical bytes should reuse the artifact record/path.

**Step 5: Implement manifest writer**

`manifest.go` should write `memory/knowledge/<key>.md` with YAML frontmatter and a short generated body. Use `gopkg.in/yaml.v3`, not hand-built YAML.

Minimum manifest frontmatter:

```yaml
kind: knowledge_source
key: pilot-contract
title: Pilot Contract
scope: global
scope_key: global
restricted: true
source_kind: pilot_contract
source_class: text
artifact_sha256: sha256:...
artifact_ref: knowledge/artifacts/...
original_path: /original/path
refresh_policy: manual
extractor: plain_text:v1
citation_policy: narrow_cited_snippets
topics: []
entities: []
related_sources: []
applies_to: []
```

**Step 6: Implement Markdown/text extractors**

`extractors.go` should support:

- `.md`, `.markdown` -> Markdown extractor
- `.txt`, `.text` -> plain text extractor

Both return extracted text, normalized Markdown, anchors when obvious, extractor name/version, and failure code on error.

**Step 7: Implement service**

`Service` should include:

```go
type Service struct {
	Store *sqlite.Store
	RepoRoot string
	RuntimeRoot string
	Now func() time.Time
}
```

Methods:

```go
func (s Service) Ingest(ctx context.Context, params IngestParams) (IngestResult, error)
func (s Service) List(ctx context.Context, params ListParams) ([]SourceView, error)
func (s Service) Show(ctx context.Context, key string) (SourceView, error)
func (s Service) Refresh(ctx context.Context, key string) (RefreshResult, error)
```

**Step 8: Run tests**

Run: `go test ./internal/memory/knowledge -count=1 -v`

Expected: PASS.

**Step 9: Commit**

```bash
git add internal/memory/knowledge
git commit -m "feat(memory): add knowledge ingest service"
```

### Task 3: Add lexical indexing and search over chunks

**Domain Goal:** Make ready Knowledge Sources searchable through local, auditable SQLite FTS before adding embeddings or graph databases.

**Domain Rules Enforced:**
- Search uses rebuildable derived chunks and FTS rows.
- Restricted sources return narrow snippets with citations by default.
- Non-ready sources are excluded by default.

**Why this matters:**
- The first useful operator value is finding cited excerpts from contracts, books, and manuals.

**Files:**
- Modify: `internal/memory/knowledge/service.go`
- Modify: `internal/memory/knowledge/extractors.go`
- Modify: `internal/memory/knowledge/service_test.go`
- Modify: `internal/store/sqlite/store.go`
- Modify: `internal/store/sqlite/store_test.go`

**Step 1: Write failing search tests**

Add:

```go
func TestServiceSearchReturnsRestrictedSnippetAndCitation(t *testing.T)
func TestServiceSearchExcludesNotReadySources(t *testing.T)
func TestServiceSearchIndexesTopicsAndEntities(t *testing.T)
```

Assert returned fields:

```go
if result.SourceKey != "pilot-contract" { ... }
if result.Restricted != true { ... }
if result.Anchor == "" { t.Fatal("missing citation anchor") }
if len(result.Snippet) > 500 { t.Fatalf("snippet too broad") }
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/knowledge ./internal/store/sqlite -run 'TestServiceSearch|TestStore.*Knowledge' -count=1 -v`

Expected: FAIL because search service behavior is incomplete.

**Step 3: Implement chunker and FTS index writes**

Use a simple deterministic v1 chunker:

- split by Markdown headings when present
- otherwise split by paragraphs
- cap chunks around 1,500-2,000 characters
- keep `ordinal` stable
- use `section:<slug>` anchors for Markdown/text

Write FTS rows with source key, title, topics, entities, and chunk text.

**Step 4: Implement search service**

Add:

```go
func (s Service) Search(ctx context.Context, params SearchParams) ([]SearchResult, error)
```

Default behavior:

- scope defaults to global/global
- limit defaults to 10
- only lifecycle `ready`
- restricted snippets capped and always include source/citation metadata

**Step 5: Run tests**

Run: `go test ./internal/memory/knowledge ./internal/store/sqlite -run 'TestServiceSearch|TestStore.*Knowledge' -count=1 -v`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/memory/knowledge internal/store/sqlite
git commit -m "feat(memory): add knowledge lexical search"
```

### Task 4: Add machine-readable PDF extraction and OCR boundary

**Domain Goal:** Support machine-readable PDFs while refusing scanned/OCR-required PDFs until a later locked decision.

**Domain Rules Enforced:**
- V1 supports machine-readable PDFs only.
- OCR-required artifacts must not become ready.
- Extractor provenance must be recorded.

**Why this matters:**
- Pilot contracts and books are often PDFs, but silent OCR or weak extraction would violate provenance and citation invariants.

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/memory/knowledge/extractors.go`
- Modify: `internal/memory/knowledge/service_test.go`
- Create: `internal/memory/knowledge/testdata/machine-readable.pdf`
- Create: `internal/memory/knowledge/testdata/ocr-required.pdf` if a small fixture can be generated or safely committed

**Step 1: Add failing PDF tests**

Add:

```go
func TestServiceIngestsMachineReadablePDF(t *testing.T)
func TestServiceDoesNotReadyOCRRequiredPDF(t *testing.T)
```

Expected machine-readable result:

```go
if result.Source.SourceClass != "machine_readable_pdf" { ... }
if result.Source.Lifecycle != "ready" { ... }
if !strings.Contains(result.ExtractedText, "Vacation") { ... }
```

Expected OCR-required result:

```go
if result.Source.Lifecycle == "ready" {
	t.Fatal("OCR-required artifact became ready")
}
if result.FailureCode != "ocr_required" {
	t.Fatalf("FailureCode = %q, want ocr_required", result.FailureCode)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/knowledge -run 'TestService.*PDF' -count=1 -v`

Expected: FAIL because PDF extraction is not implemented.

**Step 3: Add PDF dependency**

Run:

```bash
go get github.com/ledongthuc/pdf@v0.0.0-20250511090121-5959a4027728
```

This package is documented on pkg.go.dev for reading PDF text. Keep the dependency pinned by `go.mod`/`go.sum`.

**Step 4: Implement PDF extractor**

Add a machine-readable PDF extractor that:

- opens the PDF artifact
- extracts plain text
- records extractor name `ledongthuc_pdf` and module version
- creates page anchors when page iteration exposes page numbers
- returns `ocr_required` when extraction yields no meaningful text
- returns `pdf_unreadable` for parse/encryption errors

Do not add OCR or call external OCR binaries.

**Step 5: Run tests**

Run: `go test ./internal/memory/knowledge -run 'TestService.*PDF|TestServiceIngests' -count=1 -v`

Expected: PASS.

**Step 6: Commit**

```bash
git add go.mod go.sum internal/memory/knowledge
git commit -m "feat(memory): add machine-readable PDF extraction"
```

### Task 5: Add top-level `odin knowledge` direct commands

**Domain Goal:** Make Knowledge Source behavior provable through the canonical `odin knowledge ...` operator surface.

**Domain Rules Enforced:**
- Real proof must use `odin knowledge ...`.
- Direct file edits and REPL-only commands are insufficient.

**Why this matters:**
- The user-visible product is the command path, not an internal service.

**Files:**
- Create: `internal/cli/commands/knowledge.go`
- Create: `internal/cli/commands/knowledge_test.go`
- Modify: `internal/app/lifecycle/run.go`
- Modify: `internal/app/lifecycle/run_test.go`

**Step 1: Write failing command parser tests**

Add tests:

```go
func TestParseKnowledgeIngestCommand(t *testing.T)
func TestParseKnowledgeSearchCommand(t *testing.T)
func TestParseKnowledgeApproveUseCommand(t *testing.T)
```

Assert:

```go
cmd, err := commands.ParseKnowledge([]string{"ingest", "/tmp/contract.pdf", "--scope", "global", "--restricted"})
if err != nil { t.Fatal(err) }
if cmd.Action != "ingest" || !cmd.Restricted { t.Fatalf("cmd=%+v", cmd) }
```

**Step 2: Write failing lifecycle tests**

Add tests that build a temp repo/runtime root and call:

```go
err := lifecycle.Run(ctx, root, []string{"knowledge", "ingest", fixturePath, "--key", "pilot-contract", "--title", "Pilot Contract", "--restricted"}, strings.NewReader(""), &out)
```

Expected output includes:

```text
source=pilot-contract lifecycle=ready restricted=true
```

**Step 3: Run tests to verify they fail**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'Test.*Knowledge' -count=1 -v`

Expected: FAIL because command package and lifecycle dispatch do not exist.

**Step 4: Implement command parsing and rendering**

`knowledge.go` should expose:

```go
func ParseKnowledge(args []string) (KnowledgeCommand, error)
func RunKnowledge(ctx context.Context, store *sqlite.Store, repoRoot string, runtimeRoot string, args []string, stdout io.Writer) error
```

Support:

- `ingest`
- `list`
- `show`
- `search`
- `refresh`
- `approve-use`

Default text output must be concise and provenance-rich. Add `--json` where the design calls for it.

**Step 5: Wire lifecycle**

In `internal/app/lifecycle/run.go`, add:

```go
case "knowledge":
	return commands.RunKnowledge(ctx, app.Store, root, cfg.RuntimeRoot, args[1:], stdout)
```

**Step 6: Run tests**

Run: `go test ./internal/cli/commands ./internal/app/lifecycle -run 'Test.*Knowledge' -count=1 -v`

Expected: PASS.

**Step 7: Commit**

```bash
git add internal/cli/commands internal/app/lifecycle
git commit -m "feat(cli): add knowledge commands"
```

### Task 6: Add Knowledge Inbox server add flow

**Domain Goal:** Make it easy to add knowledge files to the server without editing manifests or artifact paths manually.

**Domain Rules Enforced:**
- Easy file transfer, explicit Odin ingest.
- Inbox ingest reuses the same service path as direct ingest.
- Unsupported/OCR-required files remain visible with evidence.

**Why this matters:**
- The operator needs to copy pilot contracts, books, and manuals to the server quickly and then let Odin govern ingestion.

**Files:**
- Modify: `internal/memory/knowledge/service.go`
- Modify: `internal/memory/knowledge/service_test.go`
- Modify: `internal/cli/commands/knowledge.go`
- Modify: `internal/cli/commands/knowledge_test.go`
- Modify: `internal/app/lifecycle/run_test.go`
- Modify: `docs/plans/2026-04-29-knowledge-hub-design.md` only if command names drift during implementation

**Step 1: Write failing inbox tests**

Add service tests:

```go
func TestServiceListsKnowledgeInbox(t *testing.T)
func TestServiceIngestsInboxFileAndMovesImportedOriginal(t *testing.T)
func TestServiceLeavesUnsupportedInboxFileRejectedWithReason(t *testing.T)
```

Add CLI tests:

```go
func TestKnowledgeInboxPathCommand(t *testing.T)
func TestKnowledgeIngestInboxCommand(t *testing.T)
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/knowledge ./internal/cli/commands ./internal/app/lifecycle -run 'Test.*Inbox|Test.*Knowledge' -count=1 -v`

Expected: FAIL because inbox support is missing.

**Step 3: Implement inbox methods**

Service methods:

```go
func (s Service) InboxPath() string
func (s Service) ListInbox(ctx context.Context) ([]InboxEntry, error)
func (s Service) IngestInbox(ctx context.Context, params IngestInboxParams) (IngestResult, error)
```

Directories:

```text
$ODIN_RUNTIME_ROOT/knowledge/inbox/
$ODIN_RUNTIME_ROOT/knowledge/imported/
$ODIN_RUNTIME_ROOT/knowledge/rejected/
```

Ingested files move to imported; rejected files move to rejected with a sidecar reason file or recorded SQLite evidence.

**Step 4: Add commands**

Commands:

```bash
odin knowledge inbox [--json]
odin knowledge inbox-path
odin knowledge ingest-inbox <name> [--scope global] [--restricted] [--title <title>]
odin knowledge ingest-inbox --all [--scope global] [--restricted]
```

**Step 5: Run tests**

Run: `go test ./internal/memory/knowledge ./internal/cli/commands ./internal/app/lifecycle -run 'Test.*Inbox|Test.*Knowledge' -count=1 -v`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/memory/knowledge internal/cli/commands internal/app/lifecycle docs/plans/2026-04-29-knowledge-hub-design.md
git commit -m "feat(knowledge): add inbox ingest flow"
```

### Task 7: Prove restricted-use approvals and JSON safety

**Domain Goal:** Ensure restricted sources cannot be broadly exported, shared, or injected into executor context without single-use approval evidence.

**Domain Rules Enforced:**
- Restricted Knowledge Use Approval is single-use and use-type scoped.
- JSON output respects restricted snippet limits unless a downstream use has explicit approval.
- Approval does not change lifecycle.

**Why this matters:**
- Pilot contracts and books must be useful for personal retrieval without becoming unrestricted context.

**Files:**
- Modify: `internal/memory/knowledge/service.go`
- Modify: `internal/memory/knowledge/service_test.go`
- Modify: `internal/cli/commands/knowledge.go`
- Modify: `internal/cli/commands/knowledge_test.go`
- Modify: `internal/store/sqlite/store_test.go`

**Step 1: Write failing approval and safety tests**

Add:

```go
func TestApproveUseRecordsSingleUseApproval(t *testing.T)
func TestApproveUseRejectsUnknownUseType(t *testing.T)
func TestRestrictedSearchJSONStillUsesNarrowSnippet(t *testing.T)
func TestApproveUseDoesNotChangeLifecycle(t *testing.T)
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/knowledge ./internal/cli/commands ./internal/store/sqlite -run 'Test.*Approve|Test.*Restricted' -count=1 -v`

Expected: FAIL until use-type validation and render limits are implemented.

**Step 3: Implement use-type validation**

Allowed values:

```go
const (
	UseBulkExport = "bulk_export"
	UseBroadExtraction = "broad_extraction"
	UseSharing = "sharing"
	UseExecutorContextInjection = "executor_context_injection"
)
```

`approve-use` must require `--reason` and record `decision=approved`, `decided_by=operator`, and evidence JSON with the command and timestamp.

**Step 4: Enforce restricted JSON snippets**

Make search rendering call one shared snippet function for text and JSON. Do not allow `--json` to bypass restricted limits.

**Step 5: Run tests**

Run: `go test ./internal/memory/knowledge ./internal/cli/commands ./internal/store/sqlite -run 'Test.*Approve|Test.*Restricted' -count=1 -v`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/memory/knowledge internal/cli/commands internal/store/sqlite
git commit -m "feat(knowledge): add restricted use approvals"
```

### Task 8: Add real Odin smoke and close documentation gaps

**Domain Goal:** Prove the v1 knowledge hub through the real `odin knowledge ...` operator path and document the actual command behavior.

**Domain Rules Enforced:**
- Real E2E proof must exercise `odin knowledge ...`.
- The command path is the product surface.
- OCR remains out of scope for v1.

**Why this matters:**
- The feature is incomplete until the operator can ingest, inspect, search, and approve use through the real binary.

**Files:**
- Create: `tests/integration/knowledge_hub_test.go`
- Modify: `README.md` or `docs/contracts/verification-model.md` only if the repo expects user-visible feature proof guidance there
- Modify: `docs/plans/2026-04-29-knowledge-hub-design.md` only if implementation names drift

**Step 1: Write failing integration test**

Add an integration test that:

```go
func TestKnowledgeHubRealCommandSmoke(t *testing.T) {
	// build or invoke lifecycle.Run against a temp ODIN_ROOT
	// run knowledge inbox-path
	// copy a fixture into inbox
	// run knowledge ingest-inbox
	// run knowledge list
	// run knowledge show
	// run knowledge search
	// run knowledge approve-use
	// assert outputs and persisted DB rows
}
```

**Step 2: Run test to verify it fails if command wiring is incomplete**

Run: `go test ./tests/integration -run TestKnowledgeHubRealCommandSmoke -count=1 -v`

Expected: FAIL until the full command path works in a temp runtime.

**Step 3: Fix command/runtime gaps only**

Do not add new domain concepts in this task. Fix only integration gaps in already-planned command/service/store behavior.

**Step 4: Run targeted suite**

Run:

```bash
go test ./internal/store/sqlite ./internal/memory/knowledge ./internal/cli/commands ./internal/app/lifecycle ./tests/integration -run 'Test.*Knowledge|TestKnowledgeHubRealCommandSmoke' -count=1 -v
```

Expected: PASS.

**Step 5: Build binary and run real command smoke**

Run:

```bash
go build -o ./bin/odin ./cmd/odin
ODIN_ROOT="$(mktemp -d)" ./bin/odin knowledge inbox-path
```

Then copy a text fixture into the printed inbox path and run:

```bash
ODIN_ROOT="$ODIN_ROOT" ./bin/odin knowledge inbox
ODIN_ROOT="$ODIN_ROOT" ./bin/odin knowledge ingest-inbox pilot-contract.txt --title "Pilot Contract" --restricted
ODIN_ROOT="$ODIN_ROOT" ./bin/odin knowledge list
ODIN_ROOT="$ODIN_ROOT" ./bin/odin knowledge show pilot-contract
ODIN_ROOT="$ODIN_ROOT" ./bin/odin knowledge search vacation
ODIN_ROOT="$ODIN_ROOT" ./bin/odin knowledge approve-use pilot-contract --use executor_context_injection --reason "fixture proof"
```

Expected:

- ingest reaches `lifecycle=ready`
- list/show include restricted flag and provenance
- search returns narrow cited snippet
- approval records evidence and does not change lifecycle

**Step 6: Run broader relevant tests**

Run:

```bash
go test ./internal/store/sqlite ./internal/memory/... ./internal/cli/commands ./internal/app/lifecycle ./tests/integration -count=1
```

Expected: PASS.

**Step 7: Commit**

```bash
git add tests/integration docs README.md internal
git commit -m "test(knowledge): add real odin smoke coverage"
```

## Review Checklist

- Domain naming matches `CONTEXT.md`: Knowledge Source, Knowledge Source Manifest, Knowledge Artifact, Restricted Knowledge Source, Knowledge Operator Surface, Knowledge Source Lifecycle, Restricted Knowledge Use Approval.
- Invariant coverage exists in tests for manifest-before-ready, artifact authority, lifecycle readiness, OCR boundary, restricted snippet limits, single-use approvals, and real command proof.
- ADR 0001 is honored: authored manifests in `memory/`, runtime/index state in SQLite, derived outputs are rebuildable.
- Boundary crossings are explicit: CLI -> knowledge service -> store/artifact/extractor; no sidecar RAG script.
- Reused repo structures are named: lifecycle dispatch, SQLite migrations/store, runtime events, `internal/memory`, `memory/`.
- Unresolved domain gaps are not hidden: OCR, embeddings, graph database, annotation UI, and exact future REPL aliases remain out of scope.
