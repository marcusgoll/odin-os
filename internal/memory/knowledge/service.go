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

const (
	defaultSearchLimit        = 10
	maxKnowledgeChunkChars    = 1800
	maxRestrictedSnippetChars = 500
)

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
	ready, err := s.Store.RecordReadyKnowledgeExtraction(ctx, sqlite.RecordReadyKnowledgeExtractionParams{
		SourceID:               source.ID,
		ArtifactID:             artifact.ID,
		Key:                    manifest.Key,
		Title:                  manifest.Title,
		Scope:                  manifest.Scope,
		ScopeKey:               manifest.ScopeKey,
		Restricted:             manifest.Restricted,
		SourceKind:             manifest.SourceKind,
		SourceClass:            manifest.SourceClass,
		ManifestPath:           manifestPath,
		ExtractorName:          extraction.ExtractorName,
		ExtractorVersion:       extraction.ExtractorVersion,
		ExtractedTextHash:      "sha256:" + hex.EncodeToString(textHash[:]),
		NormalizedMarkdownPath: normalizedPath,
		StartedAt:              &now,
		FinishedAt:             &now,
		Chunks:                 extractionChunks(source.ID, 0, extraction, manifest.Restricted),
	})
	if err != nil {
		return IngestResult{}, err
	}
	if err := s.indexReadyKnowledgeMetadata(ctx, ready.Chunks, manifest.Topics, manifest.Entities); err != nil {
		return IngestResult{}, err
	}
	return IngestResult{
		Source:                 sourceFromStore(ready.Source),
		Artifact:               artifact,
		Extraction:             ready.Extraction,
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
	manifestPath := canonicalManifestPath(source.Key)
	manifest, err := s.readManifest(manifestPath)
	if err != nil {
		return RefreshResult{}, err
	}
	artifact, err := s.Store.GetKnowledgeArtifact(ctx, *source.CurrentArtifactID)
	if err != nil {
		return RefreshResult{}, err
	}
	if manifest.ArtifactSHA256 != artifact.SHA256 {
		return RefreshResult{}, fmt.Errorf("knowledge manifest artifact_sha256 = %q, want %q", manifest.ArtifactSHA256, artifact.SHA256)
	}
	sourceClass := SourceClass(manifest.SourceClass)
	if err := validateTask2SourceClass(sourceClass); err != nil {
		return RefreshResult{}, err
	}
	extraction, err := extractSource(artifact.ArtifactPath, sourceClass)
	if err != nil {
		return RefreshResult{}, err
	}
	if manifest.Extractor != extraction.Extractor() {
		return RefreshResult{}, fmt.Errorf("knowledge manifest extractor = %q, want %q", manifest.Extractor, extraction.Extractor())
	}
	normalizedPath, err := s.writeNormalizedMarkdown(manifest.Key, extraction.NormalizedMarkdown)
	if err != nil {
		return RefreshResult{}, err
	}
	textHash := sha256.Sum256([]byte(extraction.Text))
	now := s.now()
	ready, err := s.Store.RecordReadyKnowledgeExtraction(ctx, sqlite.RecordReadyKnowledgeExtractionParams{
		SourceID:               source.ID,
		ArtifactID:             artifact.ID,
		Key:                    manifest.Key,
		Title:                  manifest.Title,
		Scope:                  manifest.Scope,
		ScopeKey:               manifest.ScopeKey,
		Restricted:             manifest.Restricted,
		SourceKind:             manifest.SourceKind,
		SourceClass:            manifest.SourceClass,
		ManifestPath:           manifestPath,
		ExtractorName:          extraction.ExtractorName,
		ExtractorVersion:       extraction.ExtractorVersion,
		ExtractedTextHash:      "sha256:" + hex.EncodeToString(textHash[:]),
		NormalizedMarkdownPath: normalizedPath,
		StartedAt:              &now,
		FinishedAt:             &now,
		Chunks:                 extractionChunks(source.ID, 0, extraction, manifest.Restricted),
	})
	if err != nil {
		return RefreshResult{}, err
	}
	if err := s.indexReadyKnowledgeMetadata(ctx, ready.Chunks, manifest.Topics, manifest.Entities); err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{
		Source:                 sourceFromStore(ready.Source),
		Artifact:               artifact,
		Extraction:             ready.Extraction,
		NormalizedMarkdownPath: normalizedPath,
	}, nil
}

func (s Service) Search(ctx context.Context, params SearchParams) ([]SearchResult, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("knowledge service store is required")
	}
	params.Query = strings.TrimSpace(params.Query)
	params.Scope = valueOrDefault(params.Scope, "global")
	params.ScopeKey = valueOrDefault(params.ScopeKey, "global")
	if params.Limit <= 0 {
		params.Limit = defaultSearchLimit
	}

	results, err := s.Store.SearchKnowledgeChunks(ctx, sqlite.SearchKnowledgeChunksParams{
		Query:    params.Query,
		Scope:    params.Scope,
		ScopeKey: params.ScopeKey,
		Limit:    params.Limit,
	})
	if err != nil {
		return nil, err
	}

	searchResults := make([]SearchResult, 0, len(results))
	for _, result := range results {
		searchResults = append(searchResults, SearchResult{
			SourceID:               result.SourceID,
			SourceKey:              result.SourceKey,
			Title:                  result.Title,
			ManifestPath:           result.ManifestPath,
			ChunkID:                result.ChunkID,
			ExtractionID:           result.ExtractionID,
			ArtifactID:             result.ArtifactID,
			ArtifactSHA256:         result.ArtifactSHA256,
			ExtractorName:          result.ExtractorName,
			ExtractorVersion:       result.ExtractorVersion,
			ExtractedTextHash:      result.ExtractedTextHash,
			NormalizedMarkdownPath: result.NormalizedMarkdownPath,
			ExtractionFinishedAt:   result.ExtractionFinishedAt,
			Snippet:                knowledgeSnippet(result.Text, result.Restricted),
			Anchor:                 result.Anchor,
			PageNumber:             result.PageNumber,
			Restricted:             result.Restricted,
			Rank:                   result.Rank,
		})
	}
	return searchResults, nil
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

func (s Service) indexReadyKnowledgeMetadata(ctx context.Context, chunks []sqlite.KnowledgeChunk, topics []string, entities []string) error {
	if len(chunks) == 0 || (len(topics) == 0 && len(entities) == 0) {
		return nil
	}
	for _, chunk := range chunks {
		if err := s.Store.IndexKnowledgeChunk(ctx, sqlite.IndexKnowledgeChunkParams{
			ChunkID:  chunk.ID,
			Topics:   topics,
			Entities: entities,
		}); err != nil {
			return err
		}
	}
	return nil
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

func extractionChunks(sourceID int64, extractionID int64, extraction extractionResult, restricted bool) []sqlite.RecordKnowledgeChunkParams {
	chunks := chunkExtraction(extraction)
	if len(chunks) == 0 {
		return nil
	}
	params := make([]sqlite.RecordKnowledgeChunkParams, 0, len(chunks))
	for ordinal, chunk := range chunks {
		params = append(params, sqlite.RecordKnowledgeChunkParams{
			SourceID:     sourceID,
			ExtractionID: extractionID,
			Ordinal:      ordinal,
			Text:         chunk.Text,
			Anchor:       chunk.Anchor,
			Restricted:   restricted,
		})
	}
	return params
}

type extractionChunk struct {
	Text   string
	Anchor string
}

func chunkExtraction(extraction extractionResult) []extractionChunk {
	normalized := strings.TrimSpace(extraction.NormalizedMarkdown)
	if hasMarkdownHeading(normalized) {
		return chunkMarkdownSections(normalized)
	}
	return chunkPlainText(extraction.Text)
}

func hasMarkdownHeading(markdown string) bool {
	for _, line := range strings.Split(markdown, "\n") {
		if markdownHeadingTitle(line) != "" {
			return true
		}
	}
	return false
}

func chunkMarkdownSections(markdown string) []extractionChunk {
	var chunks []extractionChunk
	var sectionLines []string
	anchor := "section:start"
	flush := func() {
		text := strings.TrimSpace(stripMarkdownMarkers(strings.Join(sectionLines, "\n")))
		chunks = appendCappedKnowledgeChunks(chunks, text, anchor)
		sectionLines = nil
	}

	for _, line := range strings.Split(markdown, "\n") {
		if title := markdownHeadingTitle(line); title != "" {
			flush()
			anchor = "section:" + slugifyKnowledgeAnchor(title)
		}
		sectionLines = append(sectionLines, line)
	}
	flush()
	return chunks
}

func markdownHeadingTitle(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return ""
	}
	hashes := 0
	for hashes < len(trimmed) && trimmed[hashes] == '#' {
		hashes++
	}
	if hashes == len(trimmed) || trimmed[hashes] != ' ' {
		return ""
	}
	return strings.TrimSpace(trimmed[hashes:])
}

func chunkPlainText(text string) []extractionChunk {
	return appendCappedKnowledgeChunks(nil, strings.TrimSpace(text), "section:"+slugifyKnowledgeAnchor(firstWords(text, 8)))
}

func appendCappedKnowledgeChunks(chunks []extractionChunk, text string, anchor string) []extractionChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return chunks
	}
	if anchor == "section:" {
		anchor = "section:start"
	}
	paragraphs := splitParagraphs(text)
	current := ""
	partIndex := 0
	for _, paragraph := range paragraphs {
		if len(paragraph) > maxKnowledgeChunkChars {
			chunks, partIndex = flushKnowledgeChunk(chunks, current, anchor, partIndex)
			current = ""
			for _, part := range splitLongText(paragraph, maxKnowledgeChunkChars) {
				chunks = append(chunks, extractionChunk{Text: part, Anchor: anchorForChunkPart(anchor, partIndex)})
				partIndex++
			}
			continue
		}
		next := paragraph
		if current != "" {
			next = current + "\n\n" + paragraph
		}
		if len(next) > maxKnowledgeChunkChars {
			chunks, partIndex = flushKnowledgeChunk(chunks, current, anchor, partIndex)
			current = paragraph
			continue
		}
		current = next
	}
	chunks, _ = flushKnowledgeChunk(chunks, current, anchor, partIndex)
	return chunks
}

func flushKnowledgeChunk(chunks []extractionChunk, text string, anchor string, partIndex int) ([]extractionChunk, int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return chunks, partIndex
	}
	return append(chunks, extractionChunk{Text: text, Anchor: anchorForChunkPart(anchor, partIndex)}), partIndex + 1
}

func splitParagraphs(text string) []string {
	parts := regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(text), -1)
	paragraphs := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			paragraphs = append(paragraphs, part)
		}
	}
	return paragraphs
}

func splitLongText(text string, limit int) []string {
	words := strings.Fields(text)
	var parts []string
	current := ""
	for _, word := range words {
		if current == "" {
			current = word
			continue
		}
		if len(current)+1+len(word) > limit {
			parts = append(parts, current)
			current = word
			continue
		}
		current += " " + word
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func anchorForChunkPart(anchor string, index int) string {
	if index == 0 {
		return anchor
	}
	return fmt.Sprintf("%s-%d", anchor, index+1)
}

func firstWords(text string, count int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return "start"
	}
	if len(words) > count {
		words = words[:count]
	}
	return strings.Join(words, " ")
}

func slugifyKnowledgeAnchor(value string) string {
	slug := strings.Trim(headingAnchorPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "-"), "-")
	if slug == "" {
		return "start"
	}
	return slug
}

func knowledgeSnippet(text string, restricted bool) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) <= maxRestrictedSnippetChars {
		return text
	}
	return strings.TrimSpace(text[:maxRestrictedSnippetChars])
}
