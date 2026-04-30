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
