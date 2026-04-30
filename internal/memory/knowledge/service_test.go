package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"odin-os/internal/store/sqlite"
)

func TestServiceIngestsTextAsGlobalRestrictedKnowledgeSource(t *testing.T) {
	ctx := context.Background()
	service, repoRoot, _ := newTestService(t)

	result, err := service.Ingest(ctx, IngestParams{
		Path:       filepath.Join("testdata", "pilot-contract.txt"),
		Key:        "pilot-contract",
		Title:      "Pilot Contract",
		Scope:      "global",
		ScopeKey:   "global",
		Restricted: true,
		SourceKind: "pilot_contract",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	if result.Source.Lifecycle != LifecycleReady {
		t.Fatalf("Lifecycle = %q, want %q", result.Source.Lifecycle, LifecycleReady)
	}
	if result.Source.ManifestPath != "memory/knowledge/pilot-contract.md" {
		t.Fatalf("ManifestPath = %q, want memory/knowledge/pilot-contract.md", result.Source.ManifestPath)
	}
	if !strings.HasPrefix(result.Artifact.SHA256, "sha256:") {
		t.Fatalf("artifact hash = %q, want sha256: prefix", result.Artifact.SHA256)
	}
	if result.Artifact.OriginalPath == "" {
		t.Fatalf("OriginalPath is empty")
	}
	if result.Extraction.ExtractorName != "plain_text" || result.Extraction.ExtractorVersion != "v1" {
		t.Fatalf("extractor = %s:%s, want plain_text:v1", result.Extraction.ExtractorName, result.Extraction.ExtractorVersion)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "memory", "knowledge", "pilot-contract.md")); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if _, err := os.Stat(result.NormalizedMarkdownPath); err != nil {
		t.Fatalf("normalized markdown missing: %v", err)
	}
}

func TestServiceIngestsMarkdownAndWritesNormalizedSnapshot(t *testing.T) {
	ctx := context.Background()
	service, repoRoot, _ := newTestService(t)

	result, err := service.Ingest(ctx, IngestParams{
		Path:       filepath.Join("testdata", "manual.md"),
		Key:        "manual",
		Title:      "Manual",
		Scope:      "global",
		ScopeKey:   "global",
		Restricted: true,
		SourceKind: "manual",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	if result.Source.Lifecycle != LifecycleReady {
		t.Fatalf("Lifecycle = %q, want %q", result.Source.Lifecycle, LifecycleReady)
	}
	if result.Source.SourceClass != SourceClassMarkdown {
		t.Fatalf("SourceClass = %q, want %q", result.Source.SourceClass, SourceClassMarkdown)
	}
	normalized, err := os.ReadFile(result.NormalizedMarkdownPath)
	if err != nil {
		t.Fatalf("ReadFile(normalized) error = %v", err)
	}
	if !strings.Contains(string(normalized), "# Normal Procedures") {
		t.Fatalf("normalized markdown = %q, want heading preserved", string(normalized))
	}
	manifest, err := os.ReadFile(filepath.Join(repoRoot, "memory", "knowledge", "manual.md"))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	for _, want := range []string{
		"kind: knowledge_source",
		"source_class: markdown",
		"extractor: markdown:v1",
		"citation_policy: narrow_cited_snippets",
	} {
		if !strings.Contains(string(manifest), want) {
			t.Fatalf("manifest missing %q:\n%s", want, string(manifest))
		}
	}
}

func TestServiceRejectsUnsupportedSourceClass(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)

	_, err := service.Ingest(ctx, IngestParams{
		Path:        filepath.Join("testdata", "manual.md"),
		Key:         "manual-pdf",
		Title:       "Manual PDF",
		Scope:       "global",
		ScopeKey:    "global",
		Restricted:  true,
		SourceKind:  "manual",
		SourceClass: "ocr_required",
	})
	if err == nil {
		t.Fatalf("Ingest() error = nil, want unsupported source class")
	}
	if !strings.Contains(err.Error(), "unsupported source class") {
		t.Fatalf("Ingest() error = %v, want unsupported source class", err)
	}
}

func TestServiceDefaultsPilotContractToRestrictedManifestAndChunks(t *testing.T) {
	ctx := context.Background()
	service, repoRoot, _ := newTestService(t)

	result, err := service.Ingest(ctx, IngestParams{
		Path:       filepath.Join("testdata", "pilot-contract.txt"),
		Key:        "pilot-contract-default",
		Title:      "Pilot Contract Default",
		Scope:      "global",
		ScopeKey:   "global",
		SourceKind: "pilot_contract",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if !result.Source.Restricted {
		t.Fatalf("Restricted = false, want default restricted")
	}

	manifest, err := os.ReadFile(filepath.Join(repoRoot, "memory", "knowledge", "pilot-contract-default.md"))
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	if !strings.Contains(string(manifest), "restricted: true") {
		t.Fatalf("manifest = %s, want restricted: true", string(manifest))
	}

	results, err := service.Store.SearchKnowledgeChunks(ctx, sqlite.SearchKnowledgeChunksParams{
		Query:    "vacation",
		Scope:    "global",
		ScopeKey: "global",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("SearchKnowledgeChunks() error = %v", err)
	}
	if len(results) != 1 || !results[0].Restricted {
		t.Fatalf("results = %+v, want one restricted chunk", results)
	}
}

func TestServiceSearchReturnsRestrictedSnippetAndCitation(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)
	sourcePath := filepath.Join(t.TempDir(), "contract.md")
	longSection := strings.Repeat("Vacation accrual rules apply to line pilots. ", 30)
	if err := os.WriteFile(sourcePath, []byte("# Vacation Rules\n\n"+longSection+"\n\n## Scheduling\n\nReserve windows stay separate.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	ingested, err := service.Ingest(ctx, IngestParams{
		Path:       sourcePath,
		Key:        "search-contract",
		Title:      "Search Contract",
		Scope:      "global",
		ScopeKey:   "global",
		Restricted: true,
		SourceKind: "pilot_contract",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	results, err := service.Search(ctx, SearchParams{Query: "vacation"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want one result", results)
	}
	result := results[0]
	if result.SourceKey != "search-contract" || result.Title != "Search Contract" {
		t.Fatalf("result = %+v, want source citation metadata", result)
	}
	if !result.Restricted {
		t.Fatalf("Restricted = false, want restricted result")
	}
	if len(result.Snippet) > 500 {
		t.Fatalf("Snippet length = %d, want <= 500", len(result.Snippet))
	}
	if !strings.Contains(strings.ToLower(result.Snippet), "vacation") {
		t.Fatalf("Snippet = %q, want query text", result.Snippet)
	}
	if result.ManifestPath != "memory/knowledge/search-contract.md" || result.Anchor != "section:vacation-rules" {
		t.Fatalf("result = %+v, want manifest path and section anchor", result)
	}
	if result.ArtifactID != ingested.Artifact.ID || result.ArtifactSHA256 != ingested.Artifact.SHA256 {
		t.Fatalf("result = %+v, want artifact provenance", result)
	}
	if result.ExtractionID != ingested.Extraction.ID || result.ExtractorName != "markdown" || result.ExtractorVersion != "v1" {
		t.Fatalf("result = %+v, want extraction provenance", result)
	}
	if result.ExtractedTextHash != ingested.Extraction.ExtractedTextHash || result.NormalizedMarkdownPath != ingested.NormalizedMarkdownPath {
		t.Fatalf("result = %+v, want extracted output provenance", result)
	}
	if result.ExtractionFinishedAt == nil {
		t.Fatalf("ExtractionFinishedAt is nil")
	}
}

func TestServiceSearchExcludesNotReadySources(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)

	artifact, err := service.Store.RecordKnowledgeArtifact(ctx, sqlite.RecordKnowledgeArtifactParams{
		SHA256:       "sha256:not-ready",
		SizeBytes:    42,
		SourceType:   "text",
		MimeType:     "text/plain",
		ArtifactPath: "knowledge/artifacts/no/not-ready/source.txt",
		OriginalPath: "/tmp/not-ready.txt",
	})
	if err != nil {
		t.Fatalf("RecordKnowledgeArtifact() error = %v", err)
	}
	source, err := service.Store.UpsertKnowledgeSource(ctx, sqlite.UpsertKnowledgeSourceParams{
		Key:               "not-ready-contract",
		Title:             "Not Ready Contract",
		Scope:             "global",
		ScopeKey:          "global",
		Restricted:        true,
		SourceKind:        "pilot_contract",
		SourceClass:       "text",
		Lifecycle:         "artifact_available",
		ManifestPath:      "memory/knowledge/not-ready-contract.md",
		CurrentArtifactID: &artifact.ID,
	})
	if err != nil {
		t.Fatalf("UpsertKnowledgeSource() error = %v", err)
	}
	extraction, err := service.Store.RecordKnowledgeExtraction(ctx, sqlite.RecordKnowledgeExtractionParams{
		SourceID:               source.ID,
		ArtifactID:             artifact.ID,
		ExtractorName:          "plain_text",
		ExtractorVersion:       "v1",
		Status:                 "succeeded",
		ExtractedTextHash:      "sha256:not-ready-text",
		NormalizedMarkdownPath: "state/knowledge/normalized/not-ready-contract.md",
	})
	if err != nil {
		t.Fatalf("RecordKnowledgeExtraction() error = %v", err)
	}
	if _, err := service.Store.RecordKnowledgeChunk(ctx, sqlite.RecordKnowledgeChunkParams{
		SourceID:     source.ID,
		ExtractionID: extraction.ID,
		Ordinal:      0,
		Text:         "Vacation clause exists but source is not ready.",
		Anchor:       "section:vacation",
		Restricted:   true,
	}); err != nil {
		t.Fatalf("RecordKnowledgeChunk() error = %v", err)
	}

	results, err := service.Search(ctx, SearchParams{Query: "vacation"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want non-ready source excluded", results)
	}
}

func TestServiceSearchIndexesTopicsAndEntities(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)

	_, err := service.Ingest(ctx, IngestParams{
		Path:       filepath.Join("testdata", "pilot-contract.txt"),
		Key:        "metadata-contract",
		Title:      "Metadata Contract",
		Scope:      "global",
		ScopeKey:   "global",
		Restricted: true,
		SourceKind: "pilot_contract",
		Topics:     []string{"vacation-bidding"},
		Entities:   []string{"PSA"},
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	for _, query := range []string{"vacation-bidding", "PSA"} {
		results, err := service.Search(ctx, SearchParams{Query: query})
		if err != nil {
			t.Fatalf("Search(%q) error = %v", query, err)
		}
		if len(results) != 1 || results[0].SourceKey != "metadata-contract" {
			t.Fatalf("Search(%q) results = %+v, want metadata-contract", query, results)
		}
	}
}

func TestServiceReusesIdenticalArtifactBytesAcrossDifferentFilenames(t *testing.T) {
	ctx := context.Background()
	service, _, runtimeRoot := newTestService(t)
	sourceDir := t.TempDir()
	firstPath := filepath.Join(sourceDir, "first.txt")
	secondPath := filepath.Join(sourceDir, "second.txt")
	bytes, err := os.ReadFile(filepath.Join("testdata", "pilot-contract.txt"))
	if err != nil {
		t.Fatalf("ReadFile(testdata) error = %v", err)
	}
	if err := os.WriteFile(firstPath, bytes, 0o644); err != nil {
		t.Fatalf("WriteFile(first) error = %v", err)
	}
	if err := os.WriteFile(secondPath, bytes, 0o644); err != nil {
		t.Fatalf("WriteFile(second) error = %v", err)
	}

	first, err := service.Ingest(ctx, IngestParams{
		Path:       firstPath,
		Key:        "first-contract",
		Title:      "First Contract",
		Scope:      "global",
		ScopeKey:   "global",
		SourceKind: "pilot_contract",
	})
	if err != nil {
		t.Fatalf("Ingest(first) error = %v", err)
	}
	second, err := service.Ingest(ctx, IngestParams{
		Path:       secondPath,
		Key:        "second-contract",
		Title:      "Second Contract",
		Scope:      "global",
		ScopeKey:   "global",
		SourceKind: "pilot_contract",
	})
	if err != nil {
		t.Fatalf("Ingest(second) error = %v", err)
	}

	if second.Artifact.ID != first.Artifact.ID || second.Artifact.ArtifactPath != first.Artifact.ArtifactPath {
		t.Fatalf("second artifact = %+v, want reused first artifact %+v", second.Artifact, first.Artifact)
	}
	if count := countFiles(t, filepath.Join(runtimeRoot, "knowledge", "artifacts")); count != 1 {
		t.Fatalf("artifact file count = %d, want 1", count)
	}
}

func TestServiceReingestPreservesPriorNormalizedSnapshot(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)
	sourcePath := filepath.Join(t.TempDir(), "manual.md")
	if err := os.WriteFile(sourcePath, []byte("# First\n\nInitial content.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(first) error = %v", err)
	}

	first, err := service.Ingest(ctx, IngestParams{
		Path:       sourcePath,
		Key:        "mutable-manual",
		Title:      "Mutable Manual",
		Scope:      "global",
		ScopeKey:   "global",
		SourceKind: "manual",
	})
	if err != nil {
		t.Fatalf("Ingest(first) error = %v", err)
	}
	firstSnapshot, err := os.ReadFile(first.NormalizedMarkdownPath)
	if err != nil {
		t.Fatalf("ReadFile(first snapshot) error = %v", err)
	}

	if err := os.WriteFile(sourcePath, []byte("# Second\n\nUpdated content.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(second) error = %v", err)
	}
	second, err := service.Ingest(ctx, IngestParams{
		Path:       sourcePath,
		Key:        "mutable-manual",
		Title:      "Mutable Manual",
		Scope:      "global",
		ScopeKey:   "global",
		SourceKind: "manual",
	})
	if err != nil {
		t.Fatalf("Ingest(second) error = %v", err)
	}

	if second.NormalizedMarkdownPath == first.NormalizedMarkdownPath {
		t.Fatalf("normalized path did not change: %s", second.NormalizedMarkdownPath)
	}
	reloadedFirstSnapshot, err := os.ReadFile(first.NormalizedMarkdownPath)
	if err != nil {
		t.Fatalf("ReadFile(first snapshot again) error = %v", err)
	}
	if string(reloadedFirstSnapshot) != string(firstSnapshot) {
		t.Fatalf("first snapshot changed from %q to %q", string(firstSnapshot), string(reloadedFirstSnapshot))
	}
}

func TestServiceRefreshUsesManifestAsSourceDeclaration(t *testing.T) {
	ctx := context.Background()
	service, repoRoot, _ := newTestService(t)

	result, err := service.Ingest(ctx, IngestParams{
		Path:       filepath.Join("testdata", "manual.md"),
		Key:        "refresh-manual",
		Title:      "Manual",
		Scope:      "global",
		ScopeKey:   "global",
		Restricted: true,
		SourceKind: "manual",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}

	manifestPath := filepath.Join(repoRoot, "memory", "knowledge", "refresh-manual.md")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile(manifest) error = %v", err)
	}
	updatedManifest := strings.Replace(string(manifest), "title: Manual", "title: Refreshed Manual", 1)
	if err := os.WriteFile(manifestPath, []byte(updatedManifest), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}

	refreshed, err := service.Refresh(ctx, "refresh-manual")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshed.Source.Title != "Refreshed Manual" {
		t.Fatalf("refreshed title = %q, want manifest title", refreshed.Source.Title)
	}
	if result.Source.CurrentExtractionID == nil || refreshed.Source.CurrentExtractionID == nil || *result.Source.CurrentExtractionID == *refreshed.Source.CurrentExtractionID {
		t.Fatalf("refresh did not promote a new extraction: before=%v after=%v", result.Source.CurrentExtractionID, refreshed.Source.CurrentExtractionID)
	}
}

func TestServiceReadyPromotionIsAtomicWhenChunksFail(t *testing.T) {
	ctx := context.Background()
	service, _, _ := newTestService(t)

	result, err := service.Ingest(ctx, IngestParams{
		Path:       filepath.Join("testdata", "pilot-contract.txt"),
		Key:        "atomic-contract",
		Title:      "Atomic Contract",
		Scope:      "global",
		ScopeKey:   "global",
		Restricted: true,
		SourceKind: "pilot_contract",
	})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if result.Source.CurrentExtractionID == nil {
		t.Fatalf("CurrentExtractionID is nil")
	}
	beforeExtractionID := *result.Source.CurrentExtractionID

	_, err = service.Store.RecordReadyKnowledgeExtraction(ctx, sqlite.RecordReadyKnowledgeExtractionParams{
		SourceID:               result.Source.ID,
		ArtifactID:             result.Artifact.ID,
		Key:                    result.Source.Key,
		Title:                  "Should Not Promote",
		Scope:                  result.Source.Scope,
		ScopeKey:               result.Source.ScopeKey,
		Restricted:             result.Source.Restricted,
		SourceKind:             result.Source.SourceKind,
		SourceClass:            string(result.Source.SourceClass),
		ManifestPath:           result.Source.ManifestPath,
		ExtractorName:          result.Extraction.ExtractorName,
		ExtractorVersion:       result.Extraction.ExtractorVersion,
		ExtractedTextHash:      result.Extraction.ExtractedTextHash,
		NormalizedMarkdownPath: result.Extraction.NormalizedMarkdownPath,
		Chunks: []sqlite.RecordKnowledgeChunkParams{{
			Ordinal:    0,
			Text:       "   ",
			Restricted: true,
		}},
	})
	if err == nil {
		t.Fatalf("RecordReadyKnowledgeExtraction() error = nil, want chunk failure")
	}

	after, err := service.Show(ctx, "atomic-contract")
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if after.Source.Title != "Atomic Contract" {
		t.Fatalf("title = %q, want prior ready source title", after.Source.Title)
	}
	if after.Source.CurrentExtractionID == nil || *after.Source.CurrentExtractionID != beforeExtractionID {
		t.Fatalf("current extraction = %v, want %d", after.Source.CurrentExtractionID, beforeExtractionID)
	}
}

func newTestService(t *testing.T) (Service, string, string) {
	t.Helper()

	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	runtimeRoot := filepath.Join(root, "runtime")
	if err := os.MkdirAll(filepath.Join(repoRoot, "memory", "knowledge"), 0o755); err != nil {
		t.Fatalf("MkdirAll(repo knowledge) error = %v", err)
	}

	store, err := sqlite.Open(filepath.Join(root, "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	store.Now = func() time.Time { return time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC) }
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	return Service{
		Store:       store,
		RepoRoot:    repoRoot,
		RuntimeRoot: runtimeRoot,
		Now:         store.Now,
	}, repoRoot, runtimeRoot
}

func countFiles(t *testing.T, root string) int {
	t.Helper()

	count := 0
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkDir(%s) error = %v", root, err)
	}
	return count
}
