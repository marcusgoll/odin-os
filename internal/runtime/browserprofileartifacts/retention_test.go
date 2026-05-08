package browserprofileartifacts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestRetentionDryRunReportsEligibleArtifactsWithoutMutatingFilesOrStatus(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	session := createTestSession(t, ctx, store)

	active := createRetentionArtifact(t, ctx, store, root, session, "active.enc")
	revoked := createRetentionArtifact(t, ctx, store, root, session, "revoked.enc")
	expired := createRetentionArtifact(t, ctx, store, root, session, "expired.enc")
	markRetentionRevoked(t, ctx, store, revoked.ID)
	markRetentionExpired(t, ctx, store, expired.ID)

	result, err := Retain(ctx, RetentionParams{
		Store:    store,
		ODINRoot: root,
		Now:      now,
	})
	if err != nil {
		t.Fatalf("Retain(dry-run) error = %v", err)
	}
	if !result.DryRun || result.Eligible != 2 || result.Cleaned != 0 || result.Failed != 0 {
		t.Fatalf("Retain(dry-run) = %+v, want two eligible dry-run artifacts", result)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("Retain(dry-run) artifacts = %d, want 2", len(result.Artifacts))
	}
	for _, item := range result.Artifacts {
		if item.Action != RetentionActionWouldClean || item.Removed {
			t.Fatalf("dry-run item = %+v, want would_clean without removal", item)
		}
	}
	for _, artifact := range []sqlite.BrowserEncryptedProfileArtifact{active, revoked, expired} {
		if _, err := os.Stat(filepath.Join(root, artifact.EncryptedArtifactPath)); err != nil {
			t.Fatalf("artifact %d missing after dry-run: %v", artifact.ID, err)
		}
	}
	assertRetentionStatus(t, ctx, store, active.ID, sqlite.BrowserEncryptedProfileArtifactStatusEncrypted)
	assertRetentionStatus(t, ctx, store, revoked.ID, sqlite.BrowserEncryptedProfileArtifactStatusRevoked)
	assertRetentionStatus(t, ctx, store, expired.ID, sqlite.BrowserEncryptedProfileArtifactStatusExpired)
}

func TestRetentionApplyDeletesEligibleArtifactsAuditsCleanupAndPreservesActiveAndNonArtifactPaths(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	now := time.Date(2026, 5, 8, 13, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	session := createTestSession(t, ctx, store)

	active := createRetentionArtifact(t, ctx, store, root, session, "active-apply.enc")
	revoked := createRetentionArtifact(t, ctx, store, root, session, "revoked-apply.enc")
	expired := createRetentionArtifact(t, ctx, store, root, session, "expired-apply.enc")
	markRetentionRevoked(t, ctx, store, revoked.ID)
	markRetentionExpired(t, ctx, store, expired.ID)
	protectedPaths := []string{
		session.ProfilePath,
		"browser-sessions/encrypted-profiles/retention.decrypted",
		"cookies",
		"credentials",
	}
	for _, rel := range protectedPaths {
		writeRetentionMarker(t, filepath.Join(root, rel))
	}

	result, err := Retain(ctx, RetentionParams{
		Store:    store,
		ODINRoot: root,
		Now:      now,
		Apply:    true,
	})
	if err != nil {
		t.Fatalf("Retain(apply) error = %v", err)
	}
	if result.DryRun || result.Eligible != 2 || result.Cleaned != 2 || result.Failed != 0 {
		t.Fatalf("Retain(apply) = %+v, want two cleaned artifacts", result)
	}
	for _, item := range result.Artifacts {
		if item.Action != RetentionActionCleaned || !item.Removed {
			t.Fatalf("apply item = %+v, want cleaned removal", item)
		}
	}
	for _, artifact := range []sqlite.BrowserEncryptedProfileArtifact{revoked, expired} {
		if _, err := os.Stat(filepath.Join(root, artifact.EncryptedArtifactPath)); !os.IsNotExist(err) {
			t.Fatalf("eligible artifact %d still exists after apply: err=%v", artifact.ID, err)
		}
		cleaned := assertRetentionStatus(t, ctx, store, artifact.ID, sqlite.BrowserEncryptedProfileArtifactStatusCleaned)
		if cleaned.CleanedAt == nil {
			t.Fatalf("artifact %d CleanedAt = nil, want cleanup timestamp", artifact.ID)
		}
	}
	assertRetentionStatus(t, ctx, store, active.ID, sqlite.BrowserEncryptedProfileArtifactStatusEncrypted)
	if _, err := os.Stat(filepath.Join(root, active.EncryptedArtifactPath)); err != nil {
		t.Fatalf("active artifact missing after retention apply: %v", err)
	}
	for _, rel := range protectedPaths {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("protected path %q was touched by retention: %v", rel, err)
		}
	}
	if countRetentionEvents(t, ctx, store, runtimeevents.EventBrowserProfileCleaned) != 2 {
		t.Fatalf("browser.profile_cleaned events = %d, want 2", countRetentionEvents(t, ctx, store, runtimeevents.EventBrowserProfileCleaned))
	}
}

func TestRetentionApplyAuditsCleanupFailureForInvalidArtifactPath(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t)
	now := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	session := createTestSession(t, ctx, store)
	artifact := createRetentionArtifact(t, ctx, store, root, session, "invalid-path.enc")
	markRetentionExpired(t, ctx, store, artifact.ID)
	if _, err := store.DB().ExecContext(ctx, `UPDATE browser_encrypted_profile_artifacts SET encrypted_artifact_path = ? WHERE id = ?`, "../outside.enc", artifact.ID); err != nil {
		t.Fatalf("corrupt artifact path error = %v", err)
	}

	result, err := Retain(ctx, RetentionParams{
		Store:    store,
		ODINRoot: root,
		Now:      now,
		Apply:    true,
	})
	if err != nil {
		t.Fatalf("Retain(apply invalid path) error = %v", err)
	}
	if result.Eligible != 1 || result.Cleaned != 0 || result.Failed != 1 {
		t.Fatalf("Retain(apply invalid path) = %+v, want one failed cleanup", result)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].Action != RetentionActionFailed || result.Artifacts[0].ErrorCode != "cleanup_failed" {
		t.Fatalf("failed retention item = %+v, want cleanup_failed", result.Artifacts)
	}
	persisted := assertRetentionStatus(t, ctx, store, artifact.ID, sqlite.BrowserEncryptedProfileArtifactStatusExpired)
	if persisted.ErrorCode == nil || *persisted.ErrorCode != "cleanup_failed" {
		t.Fatalf("persisted error_code = %v, want cleanup_failed", persisted.ErrorCode)
	}
	if countRetentionEvents(t, ctx, store, runtimeevents.EventBrowserProfileCleanupFailed) != 1 {
		t.Fatalf("browser.profile_cleanup_failed events = %d, want 1", countRetentionEvents(t, ctx, store, runtimeevents.EventBrowserProfileCleanupFailed))
	}
}

func createRetentionArtifact(t *testing.T, ctx context.Context, store *sqlite.Store, root string, session sqlite.BrowserSession, name string) sqlite.BrowserEncryptedProfileArtifact {
	t.Helper()
	path := filepath.ToSlash(filepath.Join("browser-sessions", "encrypted-profiles", name))
	writeRetentionMarker(t, filepath.Join(root, path))
	artifact, err := store.CreateBrowserEncryptedProfileArtifact(ctx, sqlite.CreateBrowserEncryptedProfileArtifactParams{
		SessionID:             session.ID,
		ProfilePath:           session.ProfilePath,
		EncryptedArtifactPath: path,
		EncryptionKeyRef:      "test-key:v1",
	})
	if err != nil {
		t.Fatalf("CreateBrowserEncryptedProfileArtifact(%s) error = %v", name, err)
	}
	return artifact
}

func writeRetentionMarker(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte("fixture marker"), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func markRetentionRevoked(t *testing.T, ctx context.Context, store *sqlite.Store, id int64) {
	t.Helper()
	if _, err := store.MarkBrowserEncryptedProfileArtifactRevoked(ctx, sqlite.MarkBrowserEncryptedProfileArtifactRevokedParams{
		ID:     id,
		Actor:  "test",
		Reason: "retention test revoke",
	}); err != nil {
		t.Fatalf("MarkBrowserEncryptedProfileArtifactRevoked(%d) error = %v", id, err)
	}
}

func markRetentionExpired(t *testing.T, ctx context.Context, store *sqlite.Store, id int64) {
	t.Helper()
	if _, err := store.MarkBrowserEncryptedProfileArtifactExpired(ctx, sqlite.MarkBrowserEncryptedProfileArtifactExpiredParams{
		ID:     id,
		Actor:  "test",
		Reason: "retention test expire",
	}); err != nil {
		t.Fatalf("MarkBrowserEncryptedProfileArtifactExpired(%d) error = %v", id, err)
	}
}

func assertRetentionStatus(t *testing.T, ctx context.Context, store *sqlite.Store, id int64, want sqlite.BrowserEncryptedProfileArtifactStatus) sqlite.BrowserEncryptedProfileArtifact {
	t.Helper()
	artifact, err := store.GetBrowserEncryptedProfileArtifact(ctx, id)
	if err != nil {
		t.Fatalf("GetBrowserEncryptedProfileArtifact(%d) error = %v", id, err)
	}
	if artifact.Status != want {
		t.Fatalf("artifact %d status = %q, want %q", id, artifact.Status, want)
	}
	return artifact
}

func countRetentionEvents(t *testing.T, ctx context.Context, store *sqlite.Store, eventType runtimeevents.Type) int {
	t.Helper()
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	count := 0
	for _, event := range events {
		if event.Type == eventType {
			count++
			payload := strings.ToLower(string(event.Payload))
			for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "credential", "profile_bytes"} {
				if strings.Contains(payload, forbidden) {
					t.Fatalf("event %s payload contains forbidden marker %q: %s", eventType, forbidden, payload)
				}
			}
		}
	}
	return count
}
