package knowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
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
	if existing, err := s.Store.GetKnowledgeArtifactBySHA(ctx, sha); err == nil {
		if err := verifyArtifactFile(existing.ArtifactPath, sha, existing.SizeBytes); err != nil {
			return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
		}
		record := artifactRecord{
			SHA256:       existing.SHA256,
			SizeBytes:    existing.SizeBytes,
			SourceType:   existing.SourceType,
			MimeType:     existing.MimeType,
			ArtifactPath: existing.ArtifactPath,
			OriginalPath: originalPath,
		}
		if ref, ok := artifactRefFromPath(runtimeRoot, existing.ArtifactPath); ok {
			record.ArtifactRef = ref
		}
		return existing, record, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
	}

	filename := filepath.Base(originalPath)
	artifactRef := filepath.ToSlash(filepath.Join("knowledge", "artifacts", hexHash[:2], hexHash, filename))
	artifactPath := filepath.Join(runtimeRoot, filepath.FromSlash(artifactRef))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
	}
	if _, err := os.Stat(artifactPath); err == nil {
		if err := verifyArtifactFile(artifactPath, sha, size); err != nil {
			return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
		}
	} else {
		if !os.IsNotExist(err) {
			return sqlite.KnowledgeArtifact{}, artifactRecord{}, err
		}
		if err := moveVerifiedTempFile(tmpName, artifactPath, sha, size); err != nil {
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
	if artifact.ArtifactPath != artifactPath {
		_ = os.Remove(artifactPath)
	}
	record.ArtifactPath = artifact.ArtifactPath
	if ref, ok := artifactRefFromPath(runtimeRoot, artifact.ArtifactPath); ok {
		record.ArtifactRef = ref
	}
	return artifact, record, nil
}

func moveVerifiedTempFile(sourcePath string, destPath string, expectedSHA string, expectedSize int64) error {
	destDir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(destDir, ".artifact-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	source, err := os.Open(sourcePath)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	defer source.Close()

	if _, err := io.Copy(tmp, source); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := verifyArtifactFile(tmpName, expectedSHA, expectedSize); err != nil {
		return err
	}
	if err := os.Link(tmpName, destPath); err != nil {
		if os.IsExist(err) {
			return verifyArtifactFile(destPath, expectedSHA, expectedSize)
		}
		return err
	}
	_ = os.Remove(tmpName)
	removeTmp = false
	return nil
}

func verifyArtifactFile(path string, expectedSHA string, expectedSize int64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return err
	}
	if size != expectedSize {
		return fmt.Errorf("knowledge artifact %s size = %d, want %d", path, size, expectedSize)
	}
	gotSHA := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if gotSHA != expectedSHA {
		return fmt.Errorf("knowledge artifact %s hash = %s, want %s", path, gotSHA, expectedSHA)
	}
	return nil
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
