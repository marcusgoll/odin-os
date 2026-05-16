package browserprofilematerialize

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"odin-os/internal/runtime/browserprofilearchive"
	"odin-os/internal/runtime/browserprofileartifacts"
	"odin-os/internal/runtime/browserprofilecrypto"
	"odin-os/internal/runtime/browserprofilekeys"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestMaterializeDecryptsFixtureArtifactIntoReadOnlyRuntimeDirAndCleanupPreservesArtifact(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openMaterializeTestStore(t)
	session := createMaterializeTestSession(t, ctx, store)
	key := bytes.Repeat([]byte{0x74}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))
	plaintext := []byte("fixture materialized profile bytes")
	artifact := writeMaterializeTestArtifact(t, ctx, store, root, session, plaintext, "materialize-roundtrip.enc")
	targetDir := "runtime/browser-profile-materializations/roundtrip"

	result, err := Materialize(ctx, Params{
		Store:       store,
		ODINRoot:    root,
		Artifact:    artifact,
		TargetDir:   targetDir,
		KeyProvider: browserprofilekeys.LoadFromEnv,
		Actor:       "test",
		Reason:      "materialization test",
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if result.ArtifactID != artifact.ID || result.SessionID != session.ID {
		t.Fatalf("result identity = %+v, want artifact/session identity", result)
	}
	if result.MaterializationPath != targetDir {
		t.Fatalf("MaterializationPath = %q, want %q", result.MaterializationPath, targetDir)
	}
	materializedAbs := filepath.Join(root, filepath.FromSlash(result.MaterializedFilePath))
	materializedBytes, err := os.ReadFile(materializedAbs)
	if err != nil {
		t.Fatalf("ReadFile(materialized) error = %v", err)
	}
	if !bytes.Equal(materializedBytes, plaintext) {
		t.Fatalf("materialized bytes = %q, want fixture plaintext", materializedBytes)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(materializedAbs)
		if err != nil {
			t.Fatalf("Stat(materialized) error = %v", err)
		}
		if info.Mode().Perm()&0o222 != 0 {
			t.Fatalf("materialized mode = %v, want read-only", info.Mode().Perm())
		}
		dirInfo, err := os.Stat(filepath.Join(root, filepath.FromSlash(targetDir)))
		if err != nil {
			t.Fatalf("Stat(materialization dir) error = %v", err)
		}
		if dirInfo.Mode().Perm()&0o222 != 0 {
			t.Fatalf("materialization dir mode = %v, want read-only", dirInfo.Mode().Perm())
		}
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(artifact.EncryptedArtifactPath))); err != nil {
		t.Fatalf("encrypted artifact missing after materialize: %v", err)
	}
	assertNoMaterializeForbiddenPaths(t, root, session.ProfilePath)
	if countMaterializeEvents(t, ctx, store, runtimeevents.EventBrowserProfileMaterialized) != 1 {
		t.Fatalf("browser.profile_materialized events = %d, want 1", countMaterializeEvents(t, ctx, store, runtimeevents.EventBrowserProfileMaterialized))
	}

	cleanup, err := Cleanup(ctx, CleanupParams{
		Store:     store,
		ODINRoot:  root,
		Artifact:  artifact,
		TargetDir: targetDir,
		Actor:     "test",
		Reason:    "materialization cleanup test",
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !cleanup.Removed || cleanup.MaterializationPath != targetDir {
		t.Fatalf("cleanup = %+v, want removed materialization path", cleanup)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(targetDir))); !os.IsNotExist(err) {
		t.Fatalf("materialization dir exists after cleanup: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(artifact.EncryptedArtifactPath))); err != nil {
		t.Fatalf("encrypted artifact missing after cleanup: %v", err)
	}
	if countMaterializeEvents(t, ctx, store, runtimeevents.EventBrowserProfileMaterializationCleaned) != 1 {
		t.Fatalf("browser.profile_materialization_cleaned events = %d, want 1", countMaterializeEvents(t, ctx, store, runtimeevents.EventBrowserProfileMaterializationCleaned))
	}

	cleanup, err = Cleanup(ctx, CleanupParams{
		Store:     store,
		ODINRoot:  root,
		Artifact:  artifact,
		TargetDir: targetDir,
		Actor:     "test",
		Reason:    "idempotent cleanup test",
	})
	if err != nil {
		t.Fatalf("Cleanup(idempotent) error = %v", err)
	}
	if cleanup.Removed {
		t.Fatalf("cleanup.Removed = true after second cleanup, want false")
	}
}

func TestMaterializeDirectoryDecryptsArchiveIntoWritableRuntimeDir(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openMaterializeTestStore(t)
	session := createMaterializeTestSession(t, ctx, store)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x71}, browserprofilecrypto.KeySize)))

	source := filepath.Join(t.TempDir(), "source-profile")
	if err := os.MkdirAll(filepath.Join(source, "Default"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Default", "Preferences"), []byte("managed-profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := browserprofilearchive.Pack(source)
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	artifact := writeMaterializeTestArtifact(t, ctx, store, root, session, archive, "directory-roundtrip.enc")

	target := "runtime/browser-profile-materializations/directory-proof"
	result, err := MaterializeDirectory(ctx, Params{
		Store:       store,
		ODINRoot:    root,
		Artifact:    artifact,
		TargetDir:   target,
		KeyProvider: browserprofilekeys.LoadFromEnv,
		Actor:       "test",
		Reason:      "test materialized browser profile directory",
	})
	if err != nil {
		t.Fatalf("MaterializeDirectory() error = %v", err)
	}
	if result.MaterializationPath != target || result.MaterializedFilePath != target || result.ReadOnly {
		t.Fatalf("result = %+v, want writable directory materialization", result)
	}
	materializedPreference := filepath.Join(root, filepath.FromSlash(target), "Default", "Preferences")
	got, err := os.ReadFile(materializedPreference)
	if err != nil {
		t.Fatalf("ReadFile(materialized preference) error = %v", err)
	}
	if string(got) != "managed-profile" {
		t.Fatalf("materialized preference = %q", got)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(target), "Default", "Session"), []byte("updated"), 0o600); err != nil {
		t.Fatalf("WriteFile(updated profile state) error = %v", err)
	}
}

func TestMaterializeRejectsUnsafeTargetsAndExistingDirectories(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openMaterializeTestStore(t)
	session := createMaterializeTestSession(t, ctx, store)
	key := bytes.Repeat([]byte{0x31}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(key))
	artifact := writeMaterializeTestArtifact(t, ctx, store, root, session, []byte("fixture"), "unsafe-targets.enc")

	cases := []struct {
		name      string
		targetDir string
		want      string
	}{
		{name: "outside root", targetDir: filepath.Join(root, "..", "outside-materialized"), want: "materialization path must stay under ODIN_ROOT"},
		{name: "encrypted artifact root", targetDir: "browser-sessions/encrypted-profiles/not-allowed", want: "materialization path must stay under runtime/browser-profile-materializations"},
		{name: "prepared profile root", targetDir: session.ProfilePath, want: "materialization path must stay under runtime/browser-profile-materializations"},
		{name: "path traversal", targetDir: "runtime/browser-profile-materializations/../escape", want: "materialization path must stay under runtime/browser-profile-materializations"},
		{name: "credential marker", targetDir: "runtime/browser-profile-materializations/cookie-store", want: "forbidden metadata marker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Materialize(ctx, Params{
				Store:       store,
				ODINRoot:    root,
				Artifact:    artifact,
				TargetDir:   tc.targetDir,
				KeyProvider: browserprofilekeys.LoadFromEnv,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Materialize() error = %v, want %q", err, tc.want)
			}
		})
	}

	existing := filepath.Join(root, "runtime", "browser-profile-materializations", "existing")
	if err := os.MkdirAll(existing, 0o700); err != nil {
		t.Fatalf("MkdirAll(existing) error = %v", err)
	}
	if _, err := Materialize(ctx, Params{
		Store:       store,
		ODINRoot:    root,
		Artifact:    artifact,
		TargetDir:   "runtime/browser-profile-materializations/existing",
		KeyProvider: browserprofilekeys.LoadFromEnv,
	}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Materialize(existing) error = %v, want already exists rejection", err)
	}
}

func TestMaterializeFailsClosedForWrongKeyAndMissingArtifact(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openMaterializeTestStore(t)
	session := createMaterializeTestSession(t, ctx, store)
	goodKey := bytes.Repeat([]byte{0x41}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(goodKey))
	artifact := writeMaterializeTestArtifact(t, ctx, store, root, session, []byte("secret fixture"), "fails-closed.enc")

	wrongKey := bytes.Repeat([]byte{0x42}, browserprofilecrypto.KeySize)
	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(wrongKey))
	if _, err := Materialize(ctx, Params{
		Store:       store,
		ODINRoot:    root,
		Artifact:    artifact,
		TargetDir:   "runtime/browser-profile-materializations/wrong-key",
		KeyProvider: browserprofilekeys.LoadFromEnv,
	}); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("Materialize(wrong key) error = %v, want authentication failure", err)
	}
	if _, err := os.Stat(filepath.Join(root, "runtime", "browser-profile-materializations", "wrong-key")); !os.IsNotExist(err) {
		t.Fatalf("wrong-key materialization path exists: err=%v", err)
	}

	t.Setenv(browserprofilekeys.EnvKeyB64, base64.StdEncoding.EncodeToString(goodKey))
	missingArtifact := artifact
	missingArtifact.EncryptedArtifactPath = "browser-sessions/encrypted-profiles/missing.enc"
	if _, err := Materialize(ctx, Params{
		Store:       store,
		ODINRoot:    root,
		Artifact:    missingArtifact,
		TargetDir:   "runtime/browser-profile-materializations/missing",
		KeyProvider: browserprofilekeys.LoadFromEnv,
	}); err == nil || !strings.Contains(err.Error(), "read artifact") {
		t.Fatalf("Materialize(missing artifact) error = %v, want missing artifact read failure", err)
	}
}

func openMaterializeTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "browser-profile-materialize.db"))
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

func createMaterializeTestSession(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.BrowserSession {
	t.Helper()
	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "Materialize Fixture",
		Domain:         "example.com",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	return session
}

func writeMaterializeTestArtifact(t *testing.T, ctx context.Context, store *sqlite.Store, root string, session sqlite.BrowserSession, plaintext []byte, name string) sqlite.BrowserEncryptedProfileArtifact {
	t.Helper()
	artifact, err := browserprofileartifacts.Write(ctx, browserprofileartifacts.Params{
		Store:        store,
		ODINRoot:     root,
		SessionID:    session.ID,
		ProfilePath:  session.ProfilePath,
		Plaintext:    plaintext,
		ArtifactPath: filepath.ToSlash(filepath.Join("browser-sessions", "encrypted-profiles", name)),
		KeyProvider:  browserprofilekeys.LoadFromEnv,
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	return artifact
}

func assertNoMaterializeForbiddenPaths(t *testing.T, root string, profilePath string) {
	t.Helper()
	for _, rel := range []string{profilePath, "cookies", "cookie", "credentials", "profile-bytes"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("forbidden path %q exists after materialize err=%v", rel, err)
		}
	}
}

func countMaterializeEvents(t *testing.T, ctx context.Context, store *sqlite.Store, eventType runtimeevents.Type) int {
	t.Helper()
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents(%s) error = %v", eventType, err)
	}
	count := 0
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}
