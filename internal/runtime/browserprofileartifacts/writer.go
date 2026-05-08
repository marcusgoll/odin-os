package browserprofileartifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"odin-os/internal/runtime/browserprofilecrypto"
	"odin-os/internal/runtime/browserprofilekeys"
	"odin-os/internal/store/sqlite"
)

type KeyProvider func() (browserprofilekeys.Material, error)

type MetadataStore interface {
	CreateBrowserEncryptedProfileArtifact(context.Context, sqlite.CreateBrowserEncryptedProfileArtifactParams) (sqlite.BrowserEncryptedProfileArtifact, error)
}

type Params struct {
	Store        MetadataStore
	ODINRoot     string
	SessionID    int64
	ProfilePath  string
	Plaintext    []byte
	ArtifactPath string
	KeyProvider  KeyProvider
}

func Write(ctx context.Context, params Params) (sqlite.BrowserEncryptedProfileArtifact, error) {
	if params.Store == nil {
		return sqlite.BrowserEncryptedProfileArtifact{}, fmt.Errorf("browser profile artifact writer store is required")
	}
	if params.KeyProvider == nil {
		return sqlite.BrowserEncryptedProfileArtifact{}, fmt.Errorf("browser profile artifact writer key provider is required")
	}
	artifactAbs, artifactRel, err := normalizeArtifactPath(params.ODINRoot, params.ArtifactPath)
	if err != nil {
		return sqlite.BrowserEncryptedProfileArtifact{}, err
	}
	if err := rejectForbiddenMetadata(params.ProfilePath, artifactRel); err != nil {
		return sqlite.BrowserEncryptedProfileArtifact{}, err
	}
	material, err := params.KeyProvider()
	if err != nil {
		return sqlite.BrowserEncryptedProfileArtifact{}, err
	}
	key := material.Bytes()
	if len(key) != browserprofilecrypto.KeySize {
		return sqlite.BrowserEncryptedProfileArtifact{}, fmt.Errorf("browser profile artifact writer key must be %d bytes", browserprofilecrypto.KeySize)
	}
	if strings.TrimSpace(material.Ref) == "" {
		return sqlite.BrowserEncryptedProfileArtifact{}, fmt.Errorf("browser profile artifact writer key reference is required")
	}
	if err := rejectForbiddenMetadata(material.Ref); err != nil {
		return sqlite.BrowserEncryptedProfileArtifact{}, err
	}
	envelope, err := browserprofilecrypto.Encrypt(key, params.Plaintext)
	if err != nil {
		return sqlite.BrowserEncryptedProfileArtifact{}, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return sqlite.BrowserEncryptedProfileArtifact{}, fmt.Errorf("browser profile artifact writer encode envelope: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(artifactAbs), 0o700); err != nil {
		return sqlite.BrowserEncryptedProfileArtifact{}, fmt.Errorf("browser profile artifact writer create artifact directory: %w", err)
	}
	if err := os.WriteFile(artifactAbs, encoded, 0o600); err != nil {
		return sqlite.BrowserEncryptedProfileArtifact{}, fmt.Errorf("browser profile artifact writer write artifact: %w", err)
	}
	artifact, err := params.Store.CreateBrowserEncryptedProfileArtifact(ctx, sqlite.CreateBrowserEncryptedProfileArtifactParams{
		SessionID:             params.SessionID,
		ProfilePath:           params.ProfilePath,
		EncryptedArtifactPath: artifactRel,
		EncryptionKeyRef:      material.Ref,
	})
	if err != nil {
		_ = os.Remove(artifactAbs)
		return sqlite.BrowserEncryptedProfileArtifact{}, err
	}
	return artifact, nil
}

func normalizeArtifactPath(root string, artifactPath string) (string, string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", "", fmt.Errorf("ODIN_ROOT is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("ODIN_ROOT absolute path: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)
	artifactPath = strings.TrimSpace(artifactPath)
	if artifactPath == "" {
		return "", "", fmt.Errorf("browser profile artifact path is required")
	}
	artifactAbs := artifactPath
	if !filepath.IsAbs(artifactAbs) {
		artifactAbs = filepath.Join(rootAbs, artifactAbs)
	}
	artifactAbs = filepath.Clean(artifactAbs)
	rel, err := filepath.Rel(rootAbs, artifactAbs)
	if err != nil {
		return "", "", fmt.Errorf("browser profile artifact path relative to ODIN_ROOT: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("browser profile artifact path must stay under ODIN_ROOT")
	}
	rel = filepath.ToSlash(rel)
	const artifactRoot = "browser-sessions/encrypted-profiles/"
	if !strings.HasPrefix(rel, artifactRoot) {
		return "", "", fmt.Errorf("browser profile artifact path must stay under browser-sessions/encrypted-profiles")
	}
	name := strings.TrimPrefix(rel, artifactRoot)
	if name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("browser profile artifact path must be one encrypted artifact file")
	}
	if !strings.HasSuffix(name, ".enc") {
		return "", "", fmt.Errorf("browser profile artifact path must end with .enc")
	}
	return artifactAbs, rel, nil
}

func rejectForbiddenMetadata(values ...string) error {
	for _, value := range values {
		lower := strings.ToLower(value)
		for _, forbidden := range []string{"password", "passkey", "totp", "backup_code", "cookie", "credential", "profile_bytes"} {
			if strings.Contains(lower, forbidden) {
				return fmt.Errorf("browser profile artifact writer forbidden metadata marker %q", forbidden)
			}
		}
	}
	return nil
}
