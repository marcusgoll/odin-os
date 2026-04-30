package knowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
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

	artifact, artifactRecord, err := s.storeArtifact(ctx, sourcePath, params.SourceClass)
	if err != nil {
		return IngestResult{}, err
	}
	extraction, err := extractSource(artifact.ArtifactPath, params.SourceClass)
	if err != nil {
		return IngestResult{}, err
	}
	manifestPath, err := s.writeManifest(params, artifactRecord, extraction)
	if err != nil {
		return IngestResult{}, err
	}
	manifest, err := s.readManifest(manifestPath)
	if err != nil {
		return IngestResult{}, err
	}
	if manifest.ArtifactSHA256 != artifact.SHA256 {
		return IngestResult{}, fmt.Errorf("knowledge manifest artifact_sha256 = %q, want %q", manifest.ArtifactSHA256, artifact.SHA256)
	}
	if manifest.Extractor != extraction.Extractor() {
		return IngestResult{}, fmt.Errorf("knowledge manifest extractor = %q, want %q", manifest.Extractor, extraction.Extractor())
	}

	source, err := s.Store.GetKnowledgeSourceByKey(ctx, manifest.Key)
	if errors.Is(err, sql.ErrNoRows) {
		source, err = s.Store.UpsertKnowledgeSource(ctx, sqlite.UpsertKnowledgeSourceParams{
			Key:               manifest.Key,
			Title:             manifest.Title,
			Scope:             manifest.Scope,
			ScopeKey:          manifest.ScopeKey,
			Restricted:        manifest.Restricted,
			SourceKind:        manifest.SourceKind,
			SourceClass:       manifest.SourceClass,
			Lifecycle:         string(LifecycleArtifactAvailable),
			ManifestPath:      manifestPath,
			CurrentArtifactID: &artifact.ID,
		})
	}
	if err != nil {
		return IngestResult{}, err
	}

	normalizedPath, err := s.writeNormalizedMarkdown(manifest.Key, extraction.NormalizedMarkdown)
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
		Lifecycle:              string(LifecycleExtracted),
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
			Restricted:   manifest.Restricted,
		}); err != nil {
			return IngestResult{}, err
		}
	}

	readySource, err := s.Store.UpsertKnowledgeSource(ctx, sqlite.UpsertKnowledgeSourceParams{
		Key:                 manifest.Key,
		Title:               manifest.Title,
		Scope:               manifest.Scope,
		ScopeKey:            manifest.ScopeKey,
		Restricted:          manifest.Restricted,
		SourceKind:          manifest.SourceKind,
		SourceClass:         manifest.SourceClass,
		Lifecycle:           string(LifecycleReady),
		ManifestPath:        manifestPath,
		CurrentArtifactID:   &artifact.ID,
		CurrentExtractionID: &recordedExtraction.ID,
	})
	if err != nil {
		return IngestResult{}, err
	}
	return IngestResult{
		Source:                 sourceFromStore(readySource),
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
	artifact, err := s.Store.GetKnowledgeArtifact(ctx, *source.CurrentArtifactID)
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
		Lifecycle:              string(LifecycleExtracted),
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

	readySource, err := s.Store.UpsertKnowledgeSource(ctx, sqlite.UpsertKnowledgeSourceParams{
		Key:                 source.Key,
		Title:               source.Title,
		Scope:               source.Scope,
		ScopeKey:            source.ScopeKey,
		Restricted:          source.Restricted,
		SourceKind:          source.SourceKind,
		SourceClass:         source.SourceClass,
		Lifecycle:           string(LifecycleReady),
		ManifestPath:        source.ManifestPath,
		CurrentArtifactID:   &artifact.ID,
		CurrentExtractionID: &recordedExtraction.ID,
	})
	if err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{
		Source:                 sourceFromStore(readySource),
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
	if !params.Restricted && restrictedByDefault(params.SourceKind) {
		params.Restricted = true
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
	sum := sha256.Sum256([]byte(normalized))
	hexHash := hex.EncodeToString(sum[:])
	path := filepath.Join(runtimeRoot, "knowledge", "normalized", hexHash[:2], hexHash, key+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) != normalized {
			return "", fmt.Errorf("knowledge normalized markdown %s content hash mismatch", path)
		}
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".normalized-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.WriteString(normalized); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Link(tmpName, path); err != nil {
		if os.IsExist(err) {
			if existing, readErr := os.ReadFile(path); readErr != nil {
				return "", readErr
			} else if string(existing) != normalized {
				return "", fmt.Errorf("knowledge normalized markdown %s content hash mismatch", path)
			}
			return path, nil
		}
		return "", err
	}
	_ = os.Remove(tmpName)
	removeTmp = false
	return path, nil
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func restrictedByDefault(sourceKind string) bool {
	switch strings.TrimSpace(sourceKind) {
	case "pilot_contract", "contract", "book", "manual", "pilot_manual":
		return true
	default:
		return false
	}
}
