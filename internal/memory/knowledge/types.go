package knowledge

import (
	"time"

	"odin-os/internal/store/sqlite"
)

type Lifecycle string

const (
	LifecycleDeclared          Lifecycle = "declared"
	LifecycleArtifactAvailable Lifecycle = "artifact_available"
	LifecycleExtracted         Lifecycle = "extracted"
	LifecycleIndexed           Lifecycle = "indexed"
	LifecycleReady             Lifecycle = "ready"
	LifecycleStale             Lifecycle = "stale"
	LifecycleFailed            Lifecycle = "failed"
)

type SourceClass string

const (
	SourceClassMarkdown           SourceClass = "markdown"
	SourceClassText               SourceClass = "text"
	SourceClassMachineReadablePDF SourceClass = "machine_readable_pdf"
)

const (
	DefaultRefreshPolicy  = "manual"
	DefaultCitationPolicy = "narrow_cited_snippets"
)

type Service struct {
	Store       *sqlite.Store
	RepoRoot    string
	RuntimeRoot string
	Now         func() time.Time
}

type IngestParams struct {
	Path           string
	Key            string
	Title          string
	Scope          string
	ScopeKey       string
	Restricted     bool
	SourceKind     string
	SourceClass    SourceClass
	RefreshPolicy  string
	CitationPolicy string
	Topics         []string
	Entities       []string
	RelatedSources []string
	AppliesTo      []string
}

type IngestResult struct {
	Source                 Source
	Artifact               sqlite.KnowledgeArtifact
	Extraction             sqlite.KnowledgeExtraction
	ManifestPath           string
	NormalizedMarkdownPath string
}

type ListParams struct {
	Scope      string
	ScopeKey   string
	Lifecycle  Lifecycle
	Restricted *bool
}

type SourceView struct {
	Source Source
}

type RefreshResult struct {
	Source                 Source
	Artifact               sqlite.KnowledgeArtifact
	Extraction             sqlite.KnowledgeExtraction
	NormalizedMarkdownPath string
}

type SearchParams struct {
	Query    string
	Scope    string
	ScopeKey string
	Limit    int
}

type SearchResult struct {
	SourceID               int64
	SourceKey              string
	Title                  string
	ManifestPath           string
	ChunkID                int64
	ExtractionID           int64
	ArtifactID             int64
	ArtifactSHA256         string
	ExtractorName          string
	ExtractorVersion       string
	ExtractedTextHash      string
	NormalizedMarkdownPath string
	ExtractionFinishedAt   *time.Time
	Snippet                string
	Anchor                 string
	PageNumber             *int64
	Restricted             bool
	Rank                   float64
}

type Source struct {
	ID                  int64
	Key                 string
	Title               string
	Scope               string
	ScopeKey            string
	Restricted          bool
	SourceKind          string
	SourceClass         SourceClass
	Lifecycle           Lifecycle
	ManifestPath        string
	CurrentArtifactID   *int64
	CurrentExtractionID *int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func sourceFromStore(source sqlite.KnowledgeSource) Source {
	return Source{
		ID:                  source.ID,
		Key:                 source.Key,
		Title:               source.Title,
		Scope:               source.Scope,
		ScopeKey:            source.ScopeKey,
		Restricted:          source.Restricted,
		SourceKind:          source.SourceKind,
		SourceClass:         SourceClass(source.SourceClass),
		Lifecycle:           Lifecycle(source.Lifecycle),
		ManifestPath:        source.ManifestPath,
		CurrentArtifactID:   source.CurrentArtifactID,
		CurrentExtractionID: source.CurrentExtractionID,
		CreatedAt:           source.CreatedAt,
		UpdatedAt:           source.UpdatedAt,
	}
}
