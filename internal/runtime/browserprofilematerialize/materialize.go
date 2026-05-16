package browserprofilematerialize

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"odin-os/internal/runtime/browserprofilearchive"
	"odin-os/internal/runtime/browserprofileartifacts"
	"odin-os/internal/store/sqlite"
)

const materializedProfileFileName = "profile.materialized"

type AuditStore interface {
	RecordBrowserProfileMaterialized(context.Context, sqlite.RecordBrowserProfileMaterializedParams) (sqlite.BrowserEncryptedProfileArtifact, error)
	RecordBrowserProfileMaterializationCleaned(context.Context, sqlite.RecordBrowserProfileMaterializationCleanedParams) (sqlite.BrowserEncryptedProfileArtifact, error)
}

type Params struct {
	Store       AuditStore
	ODINRoot    string
	Artifact    sqlite.BrowserEncryptedProfileArtifact
	TargetDir   string
	KeyProvider browserprofileartifacts.KeyProvider
	Actor       string
	Reason      string
}

type Result struct {
	ArtifactID           int64  `json:"artifact_id"`
	SessionID            int64  `json:"session_id"`
	ArtifactPath         string `json:"artifact_path"`
	MaterializationPath  string `json:"materialization_path"`
	MaterializedFilePath string `json:"materialized_file_path"`
	ReadOnly             bool   `json:"read_only"`
}

type CleanupParams struct {
	Store     AuditStore
	ODINRoot  string
	Artifact  sqlite.BrowserEncryptedProfileArtifact
	TargetDir string
	Actor     string
	Reason    string
}

type CleanupResult struct {
	ArtifactID          int64  `json:"artifact_id"`
	SessionID           int64  `json:"session_id"`
	ArtifactPath        string `json:"artifact_path"`
	MaterializationPath string `json:"materialization_path"`
	Removed             bool   `json:"removed"`
}

func Materialize(ctx context.Context, params Params) (Result, error) {
	if params.Store == nil {
		return Result{}, fmt.Errorf("browser profile materialization store is required")
	}
	if params.KeyProvider == nil {
		return Result{}, fmt.Errorf("browser profile materialization key provider is required")
	}
	if params.Artifact.ID <= 0 {
		return Result{}, fmt.Errorf("browser encrypted profile artifact id must be positive")
	}
	materializationAbs, materializationRel, err := normalizeMaterializationPath(params.ODINRoot, params.TargetDir)
	if err != nil {
		return Result{}, err
	}
	if err := rejectMaterializationMetadata(params.Artifact.ProfilePath, params.Artifact.EncryptedArtifactPath, materializationRel); err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(materializationAbs); err == nil {
		return Result{}, fmt.Errorf("browser profile materialization path already exists")
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("browser profile materialization path stat: %w", err)
	}
	plaintext, err := browserprofileartifacts.Read(browserprofileartifacts.ReadParams{
		ODINRoot:     params.ODINRoot,
		ArtifactPath: params.Artifact.EncryptedArtifactPath,
		KeyProvider:  params.KeyProvider,
	})
	if err != nil {
		return Result{}, fmt.Errorf("read artifact for browser profile materialization: %w", err)
	}
	if err := os.MkdirAll(materializationAbs, 0o700); err != nil {
		return Result{}, fmt.Errorf("browser profile materialization create directory: %w", err)
	}
	materializedAbs := filepath.Join(materializationAbs, materializedProfileFileName)
	materializedRel := filepath.ToSlash(filepath.Join(materializationRel, materializedProfileFileName))
	if err := os.WriteFile(materializedAbs, plaintext, 0o600); err != nil {
		_, _ = cleanupMaterializationPath(materializationAbs)
		return Result{}, fmt.Errorf("browser profile materialization write fixture: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(materializedAbs, 0o444); err != nil {
			_, _ = cleanupMaterializationPath(materializationAbs)
			return Result{}, fmt.Errorf("browser profile materialization mark file read-only: %w", err)
		}
		if err := os.Chmod(materializationAbs, 0o555); err != nil {
			_, _ = cleanupMaterializationPath(materializationAbs)
			return Result{}, fmt.Errorf("browser profile materialization mark directory read-only: %w", err)
		}
	}
	result := Result{
		ArtifactID:           params.Artifact.ID,
		SessionID:            params.Artifact.SessionID,
		ArtifactPath:         params.Artifact.EncryptedArtifactPath,
		MaterializationPath:  materializationRel,
		MaterializedFilePath: materializedRel,
		ReadOnly:             runtime.GOOS != "windows",
	}
	if _, err := params.Store.RecordBrowserProfileMaterialized(ctx, sqlite.RecordBrowserProfileMaterializedParams{
		ID:                   params.Artifact.ID,
		MaterializationPath:  result.MaterializationPath,
		MaterializedFilePath: result.MaterializedFilePath,
		Actor:                params.Actor,
		Reason:               params.Reason,
	}); err != nil {
		_, _ = cleanupMaterializationPath(materializationAbs)
		return Result{}, err
	}
	return result, nil
}

func MaterializeDirectory(ctx context.Context, params Params) (Result, error) {
	if params.Store == nil {
		return Result{}, fmt.Errorf("browser profile materialization store is required")
	}
	if params.KeyProvider == nil {
		return Result{}, fmt.Errorf("browser profile materialization key provider is required")
	}
	if params.Artifact.ID <= 0 {
		return Result{}, fmt.Errorf("browser encrypted profile artifact id must be positive")
	}
	materializationAbs, materializationRel, err := normalizeMaterializationPath(params.ODINRoot, params.TargetDir)
	if err != nil {
		return Result{}, err
	}
	if err := rejectMaterializationMetadata(params.Artifact.ProfilePath, params.Artifact.EncryptedArtifactPath, materializationRel); err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(materializationAbs); err == nil {
		return Result{}, fmt.Errorf("browser profile materialization path already exists")
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("browser profile materialization path stat: %w", err)
	}
	plaintext, err := browserprofileartifacts.Read(browserprofileartifacts.ReadParams{
		ODINRoot:     params.ODINRoot,
		ArtifactPath: params.Artifact.EncryptedArtifactPath,
		KeyProvider:  params.KeyProvider,
	})
	if err != nil {
		return Result{}, fmt.Errorf("read artifact for browser profile materialization: %w", err)
	}
	if err := browserprofilearchive.Unpack(plaintext, materializationAbs); err != nil {
		_, _ = cleanupMaterializationPath(materializationAbs)
		return Result{}, fmt.Errorf("browser profile materialization unpack directory archive: %w", err)
	}
	result := Result{
		ArtifactID:           params.Artifact.ID,
		SessionID:            params.Artifact.SessionID,
		ArtifactPath:         params.Artifact.EncryptedArtifactPath,
		MaterializationPath:  materializationRel,
		MaterializedFilePath: materializationRel,
		ReadOnly:             false,
	}
	if _, err := params.Store.RecordBrowserProfileMaterialized(ctx, sqlite.RecordBrowserProfileMaterializedParams{
		ID:                   params.Artifact.ID,
		MaterializationPath:  result.MaterializationPath,
		MaterializedFilePath: result.MaterializedFilePath,
		Actor:                params.Actor,
		Reason:               params.Reason,
	}); err != nil {
		_, _ = cleanupMaterializationPath(materializationAbs)
		return Result{}, err
	}
	return result, nil
}

func Cleanup(ctx context.Context, params CleanupParams) (CleanupResult, error) {
	if params.Store == nil {
		return CleanupResult{}, fmt.Errorf("browser profile materialization store is required")
	}
	if params.Artifact.ID <= 0 {
		return CleanupResult{}, fmt.Errorf("browser encrypted profile artifact id must be positive")
	}
	materializationAbs, materializationRel, err := normalizeMaterializationPath(params.ODINRoot, params.TargetDir)
	if err != nil {
		return CleanupResult{}, err
	}
	if err := rejectMaterializationMetadata(params.Artifact.ProfilePath, params.Artifact.EncryptedArtifactPath, materializationRel); err != nil {
		return CleanupResult{}, err
	}
	removed, err := cleanupMaterializationPath(materializationAbs)
	if err != nil {
		return CleanupResult{}, err
	}
	result := CleanupResult{
		ArtifactID:          params.Artifact.ID,
		SessionID:           params.Artifact.SessionID,
		ArtifactPath:        params.Artifact.EncryptedArtifactPath,
		MaterializationPath: materializationRel,
		Removed:             removed,
	}
	if _, err := params.Store.RecordBrowserProfileMaterializationCleaned(ctx, sqlite.RecordBrowserProfileMaterializationCleanedParams{
		ID:                  params.Artifact.ID,
		MaterializationPath: result.MaterializationPath,
		Removed:             result.Removed,
		Actor:               params.Actor,
		Reason:              params.Reason,
	}); err != nil {
		return CleanupResult{}, err
	}
	return result, nil
}

func normalizeMaterializationPath(root string, targetDir string) (string, string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", "", fmt.Errorf("ODIN_ROOT is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("ODIN_ROOT absolute path: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" {
		return "", "", fmt.Errorf("browser profile materialization path is required")
	}
	targetAbs := targetDir
	if !filepath.IsAbs(targetAbs) {
		targetAbs = filepath.Join(rootAbs, targetAbs)
	}
	targetAbs = filepath.Clean(targetAbs)
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", "", fmt.Errorf("browser profile materialization path relative to ODIN_ROOT: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("browser profile materialization path must stay under ODIN_ROOT")
	}
	rel = filepath.ToSlash(rel)
	const materializationRoot = "runtime/browser-profile-materializations/"
	if !strings.HasPrefix(rel, materializationRoot) {
		return "", "", fmt.Errorf("browser profile materialization path must stay under runtime/browser-profile-materializations")
	}
	name := strings.TrimPrefix(rel, materializationRoot)
	if name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("browser profile materialization path must be one materialization directory")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return "", "", fmt.Errorf("browser profile materialization path contains unsafe component %q", name)
		}
	}
	return targetAbs, rel, nil
}

func cleanupMaterializationPath(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("browser profile materialization cleanup stat: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return os.Chmod(current, 0o700)
			}
			return os.Chmod(current, 0o600)
		}); err != nil {
			return false, fmt.Errorf("browser profile materialization cleanup prepare writable: %w", err)
		}
	}
	if err := os.RemoveAll(path); err != nil {
		return false, fmt.Errorf("browser profile materialization cleanup remove: %w", err)
	}
	return true, nil
}

func rejectMaterializationMetadata(values ...string) error {
	for _, value := range values {
		lower := strings.ToLower(value)
		for _, forbidden := range []string{"password", "passkey", "totp", "backup_code", "cookie", "credential", "profile_bytes"} {
			if strings.Contains(lower, forbidden) {
				return fmt.Errorf("browser profile materialization forbidden metadata marker %q", forbidden)
			}
		}
	}
	return nil
}
