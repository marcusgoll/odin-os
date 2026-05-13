package browserprofileartifacts

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/runtime/browserprofilecrypto"
	"odin-os/internal/runtime/browserprofilekeys"
)

func TestCleanupRemovesEncryptedFixtureArtifactIdempotently(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	session := createTestSession(t, ctx, store)
	key := bytes.Repeat([]byte{0x71}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))
	artifactPath := filepath.Join(root, "browser-sessions", "encrypted-profiles", "cleanup.enc")

	artifact, err := Write(ctx, Params{
		Store:        store,
		ODINRoot:     root,
		SessionID:    session.ID,
		ProfilePath:  session.ProfilePath,
		Plaintext:    []byte("fixture profile archive"),
		ArtifactPath: artifactPath,
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("artifact missing before cleanup: %v", err)
	}

	result, err := Cleanup(CleanupParams{
		ODINRoot:     root,
		ArtifactPath: artifact.EncryptedArtifactPath,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !result.Removed || result.ArtifactPath != artifact.EncryptedArtifactPath {
		t.Fatalf("Cleanup() = %+v, want removed artifact path", result)
	}
	if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Fatalf("artifact exists after cleanup: err=%v", err)
	}

	result, err = Cleanup(CleanupParams{
		ODINRoot:     root,
		ArtifactPath: artifact.EncryptedArtifactPath,
	})
	if err != nil {
		t.Fatalf("Cleanup(second) error = %v", err)
	}
	if result.Removed {
		t.Fatalf("Cleanup(second).Removed = true, want idempotent no-op")
	}
	assertNoCleanupSideEffects(t, root, session.ProfilePath)
}

func TestCleanupRejectsArtifactPathOutsideODINRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "browser-sessions", "encrypted-profiles", "outside.enc")
	if err := os.MkdirAll(filepath.Dir(outside), 0o700); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}
	if err := os.WriteFile(outside, []byte("encrypted fixture"), 0o600); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}

	_, err := Cleanup(CleanupParams{
		ODINRoot:     root,
		ArtifactPath: outside,
	})
	if err == nil || !strings.Contains(err.Error(), "ODIN_ROOT") {
		t.Fatalf("Cleanup(outside) error = %v, want ODIN_ROOT rejection", err)
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("outside artifact was modified after rejection: err=%v", statErr)
	}
}

func TestCleanupRejectsCredentialLookingArtifactPath(t *testing.T) {
	root := t.TempDir()
	artifactPath := filepath.Join(root, "browser-sessions", "encrypted-profiles", "cookies.enc")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o700); err != nil {
		t.Fatalf("MkdirAll(artifact) error = %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("encrypted fixture"), 0o600); err != nil {
		t.Fatalf("WriteFile(artifact) error = %v", err)
	}

	_, err := Cleanup(CleanupParams{
		ODINRoot:     root,
		ArtifactPath: artifactPath,
	})
	if err == nil || !strings.Contains(err.Error(), "forbidden metadata") {
		t.Fatalf("Cleanup(cookies) error = %v, want forbidden metadata rejection", err)
	}
	if _, statErr := os.Stat(artifactPath); statErr != nil {
		t.Fatalf("credential-looking artifact was modified after rejection: err=%v", statErr)
	}
}

func TestCleanupDoesNotTouchProfileCredentialOrDecryptedPaths(t *testing.T) {
	root := t.TempDir()
	profilePath := "browser-sessions/profiles/cleanup-profile"
	paths := []string{
		profilePath,
		"browser-sessions/encrypted-profiles/keep.decrypted",
		"cookies",
		"credentials",
	}
	for _, rel := range paths {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte("fixture marker"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", rel, err)
		}
	}
	artifactPath := filepath.Join(root, "browser-sessions", "encrypted-profiles", "cleanup-side-effects.enc")
	if err := os.WriteFile(artifactPath, []byte("encrypted fixture"), 0o600); err != nil {
		t.Fatalf("WriteFile(artifact) error = %v", err)
	}

	result, err := Cleanup(CleanupParams{
		ODINRoot:     root,
		ArtifactPath: artifactPath,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !result.Removed {
		t.Fatalf("Cleanup().Removed = false, want encrypted artifact removed")
	}
	for _, rel := range paths {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("non-artifact path %q was touched by cleanup: err=%v", rel, err)
		}
	}
}

func assertNoCleanupSideEffects(t *testing.T, root string, profilePath string) {
	t.Helper()
	for _, rel := range []string{
		strings.TrimSuffix("browser-sessions/encrypted-profiles/cleanup.enc", ".enc") + ".decrypted",
		profilePath,
		"cookies",
		"credentials",
		"profile",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Fatalf("unexpected cleanup side effect path %q exists: err=%v", rel, err)
		}
	}
}
