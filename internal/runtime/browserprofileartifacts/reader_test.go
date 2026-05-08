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

func TestReaderDecryptsWriterFixtureArtifact(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	session := createTestSession(t, ctx, store)
	key := bytes.Repeat([]byte{0x31}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))
	plaintext := []byte("fixture plaintext for encrypted artifact reader")
	artifactPath := filepath.Join(root, "browser-sessions", "encrypted-profiles", "reader-roundtrip.enc")

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

	readPlaintext, err := Read(ReadParams{
		ODINRoot:     root,
		ArtifactPath: artifact.EncryptedArtifactPath,
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !bytes.Equal(readPlaintext, plaintext) {
		t.Fatalf("Read() plaintext = %q, want fixture plaintext", readPlaintext)
	}
	assertNoDecryptedOrBrowserArtifacts(t, root, session.ProfilePath)
}

func TestReaderRejectsWrongKey(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	session := createTestSession(t, ctx, store)
	writeKey := bytes.Repeat([]byte{0x41}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(writeKey))
	artifactPath := filepath.Join(root, "browser-sessions", "encrypted-profiles", "wrong-key.enc")
	artifact, err := Write(ctx, Params{
		Store:        store,
		ODINRoot:     root,
		SessionID:    session.ID,
		ProfilePath:  session.ProfilePath,
		Plaintext:    []byte("fixture"),
		ArtifactPath: artifactPath,
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	wrongKey := bytes.Repeat([]byte{0x42}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(wrongKey))

	_, err = Read(ReadParams{
		ODINRoot:     root,
		ArtifactPath: artifact.EncryptedArtifactPath,
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "authentication") {
		t.Fatalf("Read() error = %v, want authentication failure", err)
	}
	assertNoDecryptedOrBrowserArtifacts(t, root, session.ProfilePath)
}

func TestReaderFailsClosedWhenKeyProviderMissing(t *testing.T) {
	root := t.TempDir()
	artifactPath := filepath.Join(root, "browser-sessions", "encrypted-profiles", "missing-key.enc")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv(browserprofilekeys.EnvKeyB64, "")

	_, err := Read(ReadParams{
		ODINRoot:     root,
		ArtifactPath: artifactPath,
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "key") {
		t.Fatalf("Read() error = %v, want missing key rejection", err)
	}
}

func TestReaderRejectsArtifactPathOutsideODINRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "browser-sessions", "encrypted-profiles", "outside.enc")

	_, err := Read(ReadParams{
		ODINRoot:     root,
		ArtifactPath: outside,
		KeyProvider: func() (browserprofilekeys.Material, error) {
			return browserprofilekeys.LoadFromEnv()
		},
	})
	if err == nil || !strings.Contains(err.Error(), "ODIN_ROOT") {
		t.Fatalf("Read() error = %v, want ODIN_ROOT path rejection", err)
	}
	if _, statErr := os.Stat(outside + ".decrypted"); !os.IsNotExist(statErr) {
		t.Fatalf("decrypted outside artifact exists after rejection: err=%v", statErr)
	}
}

func TestReaderRejectsCorruptedArtifact(t *testing.T) {
	root := t.TempDir()
	key := bytes.Repeat([]byte{0x51}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))
	artifactPath := filepath.Join(root, "browser-sessions", "encrypted-profiles", "corrupted.enc")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Read(ReadParams{
		ODINRoot:     root,
		ArtifactPath: artifactPath,
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "decode") {
		t.Fatalf("Read() error = %v, want decode rejection", err)
	}
	if _, statErr := os.Stat(strings.TrimSuffix(artifactPath, ".enc") + ".decrypted"); !os.IsNotExist(statErr) {
		t.Fatalf("decrypted artifact exists after corrupted read: err=%v", statErr)
	}
}

func assertNoDecryptedOrBrowserArtifacts(t *testing.T, root string, profilePath string) {
	t.Helper()
	for _, rel := range []string{
		"browser-sessions/encrypted-profiles/reader-roundtrip.decrypted",
		"browser-sessions/encrypted-profiles/wrong-key.decrypted",
		profilePath,
		"cookies",
		"credentials",
		"profile",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Fatalf("unexpected decrypted/browser path %q exists: err=%v", rel, err)
		}
	}
}
