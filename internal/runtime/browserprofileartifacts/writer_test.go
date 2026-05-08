package browserprofileartifacts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/runtime/browserprofilecrypto"
	"odin-os/internal/runtime/browserprofilekeys"
	"odin-os/internal/store/sqlite"
)

func TestWriterEncryptsFixtureBytesAndPersistsMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	session := createTestSession(t, ctx, store)
	key := bytes.Repeat([]byte{0x64}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))
	plaintext := []byte("fixture browser profile archive bytes")
	artifactPath := filepath.Join(root, "browser-sessions", "encrypted-profiles", "session-profile.enc")

	artifact, err := Write(ctx, Params{
		Store:        store,
		ODINRoot:     root,
		SessionID:    session.ID,
		ProfilePath:  session.ProfilePath,
		Plaintext:    plaintext,
		ArtifactPath: artifactPath,
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if artifact.ID <= 0 || artifact.SessionID != session.ID {
		t.Fatalf("artifact identity = %+v, want persisted session artifact", artifact)
	}
	if artifact.EncryptedArtifactPath != "browser-sessions/encrypted-profiles/session-profile.enc" {
		t.Fatalf("artifact.EncryptedArtifactPath = %q, want encrypted profile root path", artifact.EncryptedArtifactPath)
	}
	if artifact.ProfilePath != session.ProfilePath {
		t.Fatalf("artifact.ProfilePath = %q, want session profile path %q", artifact.ProfilePath, session.ProfilePath)
	}
	if artifact.EncryptionKeyRef != "env:"+browserprofilekeys.EnvKeyB64 {
		t.Fatalf("artifact.EncryptionKeyRef = %q, want env key ref", artifact.EncryptionKeyRef)
	}

	artifactBytes, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("ReadFile(artifact) error = %v", err)
	}
	if bytes.Contains(artifactBytes, plaintext) {
		t.Fatalf("encrypted artifact contains fixture plaintext")
	}
	var envelope browserprofilecrypto.Envelope
	if err := json.Unmarshal(artifactBytes, &envelope); err != nil {
		t.Fatalf("Unmarshal(envelope) error = %v", err)
	}
	decrypted, err := browserprofilecrypto.Decrypt(key, envelope)
	if err != nil {
		t.Fatalf("Decrypt(envelope) error = %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted fixture = %q, want plaintext", decrypted)
	}

	persisted, err := store.GetBrowserEncryptedProfileArtifact(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("GetBrowserEncryptedProfileArtifact() error = %v", err)
	}
	if persisted.EncryptedArtifactPath != artifact.EncryptedArtifactPath || persisted.EncryptionKeyRef != artifact.EncryptionKeyRef {
		t.Fatalf("persisted artifact = %+v, want writer metadata %+v", persisted, artifact)
	}
	if _, err := os.Stat(filepath.Join(root, session.ProfilePath)); !os.IsNotExist(err) {
		t.Fatalf("session profile path exists after fixture writer: err=%v", err)
	}
	for _, forbidden := range []string{"cookies", "credentials", "profile"} {
		if _, err := os.Stat(filepath.Join(root, forbidden)); !os.IsNotExist(err) {
			t.Fatalf("forbidden top-level path %q exists after fixture writer: err=%v", forbidden, err)
		}
	}
}

func TestWriterFailsClosedWhenKeyProviderMissing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	session := createTestSession(t, ctx, store)
	artifactPath := filepath.Join(root, "browser-sessions", "encrypted-profiles", "missing-key.enc")
	t.Setenv(browserprofilekeys.EnvKeyB64, "")

	_, err := Write(ctx, Params{
		Store:        store,
		ODINRoot:     root,
		SessionID:    session.ID,
		ProfilePath:  session.ProfilePath,
		Plaintext:    []byte("fixture"),
		ArtifactPath: artifactPath,
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "key") {
		t.Fatalf("Write() error = %v, want missing key rejection", err)
	}
	if _, statErr := os.Stat(artifactPath); !os.IsNotExist(statErr) {
		t.Fatalf("artifact exists after missing-key rejection: err=%v", statErr)
	}
}

func TestWriterRejectsArtifactPathOutsideODINRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	session := createTestSession(t, ctx, store)
	outside := filepath.Join(t.TempDir(), "browser-sessions", "encrypted-profiles", "outside.enc")

	_, err := Write(ctx, Params{
		Store:        store,
		ODINRoot:     root,
		SessionID:    session.ID,
		ProfilePath:  session.ProfilePath,
		Plaintext:    []byte("fixture"),
		ArtifactPath: outside,
		KeyProvider: func() (browserprofilekeys.Material, error) {
			return browserprofilekeys.LoadFromEnv()
		},
	})
	if err == nil || !strings.Contains(err.Error(), "ODIN_ROOT") {
		t.Fatalf("Write() error = %v, want ODIN_ROOT path rejection", err)
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Fatalf("outside artifact exists after rejection: err=%v", statErr)
	}
}

func TestWriterRejectsCredentialLookingMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	session := createTestSession(t, ctx, store)
	key := bytes.Repeat([]byte{0x23}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))

	_, err := Write(ctx, Params{
		Store:        store,
		ODINRoot:     root,
		SessionID:    session.ID,
		ProfilePath:  session.ProfilePath,
		Plaintext:    []byte("fixture"),
		ArtifactPath: filepath.Join(root, "browser-sessions", "encrypted-profiles", "cookies.enc"),
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err == nil || !strings.Contains(err.Error(), "forbidden metadata") {
		t.Fatalf("Write() error = %v, want forbidden metadata rejection", err)
	}
}

func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "browser-profile-artifacts.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func createTestSession(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.BrowserSession {
	t.Helper()
	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "Fixture Profile",
		Domain:         "example.com",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	return session
}
