package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"odin-os/internal/store/sqlite"
)

var sourceKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

func (s Service) Ingest(ctx context.Context, params IngestParams) (IngestResult, error) {
	params, sourcePath, err := s.normalizeIngestParams(params)
	if err != nil {
		return IngestResult{}, err
	}

	extraction, err := extractSource(sourcePath, params.SourceClass)
	if err != nil {
		return IngestResult{}, err
	}
	artifact, artifactRecord, err := s.storeArtifact(ctx, sourcePath, params.SourceClass)
	if err != nil {
		return IngestResult{}, err
	}
	manifestPath, err := s.writeManifest(params, artifactRecord, extraction)
	if err != nil {
		return IngestResult{}, err
	}

	source, err := s.Store.UpsertKnowledgeSource(ctx, sqlite.UpsertKnowledgeSourceParams{
		Key:               params.Key,
		Title:             params.Title,
		Scope:             params.Scope,
		ScopeKey:          params.ScopeKey,
		Restricted:        params.Restricted,
		SourceKind:        params.SourceKind,
		SourceClass:       string(params.SourceClass),
		Lifecycle:         string(LifecycleArtifactAvailable),
		ManifestPath:      manifestPath,
		CurrentArtifactID: &artifact.ID,
	})
	if err != nil {
		return IngestResult{}, err
	}

	normalizedPath, err := s.writeNormalizedMarkdown(params.Key, extraction.NormalizedMarkdown)
	if err != nil {
		return IngestResult{}, err
	}
	textHash := sha256.Sum256([]byte(extraction.Text))
	now := s.now()
	recordedExtraction, err := s.Store.RecordKnowledgeExtraction(ctx, sqlite.RecordKnowledgeExtractionParams{
		SourceID:               source.ID,
		ArtifactID:             artifact.ID,
		ExtractorName:          extraction.ExtractorName,
		ExtractorVersion:       extraction.ExtractorVersion,
		Status:                 "succeeded",
		Lifecycle:              string(LifecycleReady),
		ExtractedTextHash:      "sha256:" + hex.EncodeToString(textHash[:]),
		NormalizedMarkdownPath: normalizedPath,
		StartedAt:              &now,
		FinishedAt:             &now,
	})
	if err != nil {
		return IngestResult{}, err
	}

	chunkText := strings.TrimSpace(extraction.Text)
	if chunkText != "" {
		anchor := ""
		if len(extraction.Anchors) > 0 {
			anchor = extraction.Anchors[0]
		}
		if _, err := s.Store.RecordKnowledgeChunk(ctx, sqlite.RecordKnowledgeChunkParams{
			SourceID:     source.ID,
			ExtractionID: recordedExtraction.ID,
			Ordinal:      0,
			Text:         chunkText,
			Anchor:       anchor,
			Restricted:   params.Restricted,
		}); err != nil {
			return IngestResult{}, err
		}
	}

	reloadedSource, err := s.Store.GetKnowledgeSourceByKey(ctx, params.Key)
	if err != nil {
		return IngestResult{}, err
	}
	return IngestResult{
		Source:                 sourceFromStore(reloadedSource),
		Artifact:               artifact,
		Extraction:             recordedExtraction,
		ManifestPath:           manifestPath,
		NormalizedMarkdownPath: normalizedPath,
	}, nil
}

func (s Service) List(ctx context.Context, params ListParams) ([]SourceView, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("knowledge service store is required")
	}
	sources, err := s.Store.ListKnowledgeSources(ctx, sqlite.ListKnowledgeSourcesParams{
		Scope:      strings.TrimSpace(params.Scope),
		ScopeKey:   strings.TrimSpace(params.ScopeKey),
		Lifecycle:  string(params.Lifecycle),
		Restricted: params.Restricted,
	})
	if err != nil {
		return nil, err
	}
	views := make([]SourceView, 0, len(sources))
	for _, source := range sources {
		views = append(views, SourceView{Source: sourceFromStore(source)})
	}
	return views, nil
}

func (s Service) Show(ctx context.Context, key string) (SourceView, error) {
	if s.Store == nil {
		return SourceView{}, fmt.Errorf("knowledge service store is required")
	}
	source, err := s.Store.GetKnowledgeSourceByKey(ctx, strings.TrimSpace(key))
	if err != nil {
		return SourceView{}, err
	}
	return SourceView{Source: sourceFromStore(source)}, nil
}

func (s Service) Refresh(ctx context.Context, key string) (RefreshResult, error) {
	if s.Store == nil {
		return RefreshResult{}, fmt.Errorf("knowledge service store is required")
	}
	source, err := s.Store.GetKnowledgeSourceByKey(ctx, strings.TrimSpace(key))
	if err != nil {
		return RefreshResult{}, err
	}
	if source.CurrentArtifactID == nil {
		return RefreshResult{}, fmt.Errorf("knowledge source %q has no current artifact", source.Key)
	}
	artifact, err := s.getArtifact(ctx, *source.CurrentArtifactID)
	if err != nil {
		return RefreshResult{}, err
	}
	sourceClass := SourceClass(source.SourceClass)
	if err := validateTask2SourceClass(sourceClass); err != nil {
		return RefreshResult{}, err
	}
	extraction, err := extractSource(artifact.ArtifactPath, sourceClass)
	if err != nil {
		return RefreshResult{}, err
	}
	normalizedPath, err := s.writeNormalizedMarkdown(source.Key, extraction.NormalizedMarkdown)
	if err != nil {
		return RefreshResult{}, err
	}
	textHash := sha256.Sum256([]byte(extraction.Text))
	now := s.now()
	recordedExtraction, err := s.Store.RecordKnowledgeExtraction(ctx, sqlite.RecordKnowledgeExtractionParams{
		SourceID:               source.ID,
		ArtifactID:             artifact.ID,
		ExtractorName:          extraction.ExtractorName,
		ExtractorVersion:       extraction.ExtractorVersion,
		Status:                 "succeeded",
		Lifecycle:              string(LifecycleReady),
		ExtractedTextHash:      "sha256:" + hex.EncodeToString(textHash[:]),
		NormalizedMarkdownPath: normalizedPath,
		StartedAt:              &now,
		FinishedAt:             &now,
	})
	if err != nil {
		return RefreshResult{}, err
	}
	if strings.TrimSpace(extraction.Text) != "" {
		anchor := ""
		if len(extraction.Anchors) > 0 {
			anchor = extraction.Anchors[0]
		}
		if _, err := s.Store.RecordKnowledgeChunk(ctx, sqlite.RecordKnowledgeChunkParams{
			SourceID:     source.ID,
			ExtractionID: recordedExtraction.ID,
			Ordinal:      0,
			Text:         strings.TrimSpace(extraction.Text),
			Anchor:       anchor,
			Restricted:   source.Restricted,
		}); err != nil {
			return RefreshResult{}, err
		}
	}
	reloadedSource, err := s.Store.GetKnowledgeSourceByKey(ctx, source.Key)
	if err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{
		Source:                 sourceFromStore(reloadedSource),
		Artifact:               artifact,
		Extraction:             recordedExtraction,
		NormalizedMarkdownPath: normalizedPath,
	}, nil
}

func (s Service) normalizeIngestParams(params IngestParams) (IngestParams, string, error) {
	if s.Store == nil {
		return IngestParams{}, "", fmt.Errorf("knowledge service store is required")
	}
	params.Key = strings.TrimSpace(params.Key)
	params.Title = strings.TrimSpace(params.Title)
	params.Scope = strings.TrimSpace(params.Scope)
	params.ScopeKey = strings.TrimSpace(params.ScopeKey)
	params.SourceKind = strings.TrimSpace(params.SourceKind)
	params.RefreshPolicy = valueOrDefault(params.RefreshPolicy, DefaultRefreshPolicy)
	params.CitationPolicy = valueOrDefault(params.CitationPolicy, DefaultCitationPolicy)
	if params.Key == "" {
		return IngestParams{}, "", fmt.Errorf("knowledge source key is required")
	}
	if !sourceKeyPattern.MatchString(params.Key) {
		return IngestParams{}, "", fmt.Errorf("knowledge source key %q must be lower-case kebab-case", params.Key)
	}
	if params.Title == "" {
		return IngestParams{}, "", fmt.Errorf("knowledge source title is required")
	}
	if params.Scope == "" {
		return IngestParams{}, "", fmt.Errorf("knowledge source scope is required")
	}
	if params.ScopeKey == "" {
		return IngestParams{}, "", fmt.Errorf("knowledge source scope key is required")
	}
	if params.SourceKind == "" {
		return IngestParams{}, "", fmt.Errorf("knowledge source kind is required")
	}
	sourcePath, err := cleanAbsPath(params.Path, "source path")
	if err != nil {
		return IngestParams{}, "", err
	}
	inferredClass, err := inferSourceClass(sourcePath)
	if params.SourceClass == "" {
		if err != nil {
			return IngestParams{}, "", err
		}
		params.SourceClass = inferredClass
	}
	if err := validateTask2SourceClass(params.SourceClass); err != nil {
		return IngestParams{}, "", err
	}
	if err == nil && params.SourceClass != inferredClass {
		return IngestParams{}, "", fmt.Errorf("source class %q does not match file extension for %q", params.SourceClass, sourcePath)
	}
	return params, sourcePath, nil
}

func (s Service) writeNormalizedMarkdown(key string, normalized string) (string, error) {
	runtimeRoot, err := cleanAbsPath(s.RuntimeRoot, "runtime root")
	if err != nil {
		return "", err
	}
	path := filepath.Join(runtimeRoot, "knowledge", "normalized", key+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(normalized), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (s Service) getArtifact(ctx context.Context, artifactID int64) (sqlite.KnowledgeArtifact, error) {
	row := s.Store.DB().QueryRowContext(ctx, `
		SELECT id, sha256, size_bytes, source_type, mime_type, artifact_path, original_path, ocr_required, recorded_at
		FROM knowledge_artifacts
		WHERE id = ?
	`, artifactID)
	var artifact sqlite.KnowledgeArtifact
	var ocrRequired int
	var recordedAt string
	if err := row.Scan(
		&artifact.ID,
		&artifact.SHA256,
		&artifact.SizeBytes,
		&artifact.SourceType,
		&artifact.MimeType,
		&artifact.ArtifactPath,
		&artifact.OriginalPath,
		&ocrRequired,
		&recordedAt,
	); err != nil {
		return sqlite.KnowledgeArtifact{}, err
	}
	parsedRecordedAt, err := time.Parse(time.RFC3339Nano, recordedAt)
	if err != nil {
		parsedRecordedAt, err = time.Parse(time.RFC3339, recordedAt)
		if err != nil {
			return sqlite.KnowledgeArtifact{}, err
		}
	}
	artifact.RecordedAt = parsedRecordedAt.UTC()
	artifact.OCRRequired = ocrRequired != 0
	return artifact, nil
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
