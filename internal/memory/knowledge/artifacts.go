package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"odin-os/internal/store/sqlite"
)

type artifactRecord struct {
	SHA256       string
	SizeBytes    int64
	SourceType   string
	MimeType     string
	ArtifactPath string
	ArtifactRef  string
	OriginalPath string
}

func (s Service) storeArtifact(ctx context.Context, sourcePath string, sourceClass SourceClass) (sqlite.KnowledgeArtifact, artifactRecord, error) {
	if s.Store == nil {
		return sqlite.KnowledgeArtifact{}, artifactRecord{}, fmt.Errorf("knowledge service store is required")
	}
	runtimeRoot, err := cleanAbsPath(s.RuntimeRoot, "runtime root")
	if err != nil {
		return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
	}
	originalPath, err := cleanAbsPath(sourcePath, "source path")
	if err != nil {
		return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
	}

	source, err := os.Open(originalPath)
	if err != nil {
		return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
	}
	defer source.Close()

	tmp, err := os.CreateTemp("", "odin-knowledge-artifact-*")
	if err != nil {
		return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), source)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
	}

	hexHash := hex.EncodeToString(hasher.Sum(nil))
	sha := "sha256:" + hexHash
	filename := filepath.Base(originalPath)
	artifactRef := filepath.ToSlash(filepath.Join("knowledge", "artifacts", hexHash[:2], hexHash, filename))
	artifactPath := filepath.Join(runtimeRoot, filepath.FromSlash(artifactRef))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
	}
	if _, err := os.Stat(artifactPath); err != nil {
		if !os.IsNotExist(err) {
			return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
		}
		if err := copyFile(tmpName, artifactPath); err != nil {
			return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
		}
	}

	record := artifactRecord{
		SHA256:       sha,
		SizeBytes:    size,
		SourceType:   string(sourceClass),
		MimeType:     mimeTypeForSourceClass(sourceClass),
		ArtifactPath: artifactPath,
		ArtifactRef:  artifactRef,
		OriginalPath: originalPath,
	}
	artifact, err := s.Store.RecordKnowledgeArtifact(ctx, sqlite.RecordKnowledgeArtifactParams{
		SHA256:       record.SHA256,
		SizeBytes:    record.SizeBytes,
		SourceType:   record.SourceType,
		MimeType:     record.MimeType,
		ArtifactPath: record.ArtifactPath,
		OriginalPath: record.OriginalPath,
		OCRRequired:  false,
	})
	if err != nil {
		return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
	}
	record.ArtifactPath = artifact.ArtifactPath
	if ref, ok := artifactRefFromPath(runtimeRoot, artifact.ArtifactPath); ok {
		record.ArtifactRef = ref
	}
	return artifact, record, nil
}

func copyFile(sourcePath string, destPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	dest, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}

	if _, err := io.Copy(dest, source); err != nil {
		_ = dest.Close()
		return err
	}
	return dest.Close()
}

func cleanAbsPath(value string, name string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("knowledge %s is required", name)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func artifactRefFromPath(runtimeRoot string, artifactPath string) (string, bool) {
	rel, err := filepath.Rel(runtimeRoot, artifactPath)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func mimeTypeForSourceClass(sourceClass SourceClass) string {
	switch sourceClass {
	case SourceClassMarkdown:
		return "text/markdown; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}
