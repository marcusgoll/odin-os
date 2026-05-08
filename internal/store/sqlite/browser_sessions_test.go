package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
)

func TestBrowserSessionMetadataLifecyclePersistsAndAudits(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "browser-sessions.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	now := time.Date(2026, 5, 6, 18, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	exists, err := store.HasTable(ctx, "browser_session_profiles")
	if err != nil {
		t.Fatalf("HasTable(browser_session_profiles) error = %v", err)
	}
	if !exists {
		t.Fatal("HasTable(browser_session_profiles) = false, want true")
	}

	expiresAt := now.Add(30 * 24 * time.Hour)
	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Marcus AA",
		Domain:         "aa.com",
		AccountHint:    "marcus-aa",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
		ExpiresAt:      &expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	if session.ID <= 0 {
		t.Fatalf("session.ID = %d, want positive", session.ID)
	}
	if session.Status != BrowserSessionStatusCreated {
		t.Fatalf("session.Status = %q, want %q", session.Status, BrowserSessionStatusCreated)
	}
	if session.Name != "Marcus AA" || session.Domain != "aa.com" || session.AccountHint != "marcus-aa" {
		t.Fatalf("session fields = %+v, want created metadata", session)
	}
	if session.PermissionTier != BrowserSessionPermissionTierAuthenticatedReadOnly {
		t.Fatalf("session.PermissionTier = %q, want authenticated read-only", session.PermissionTier)
	}
	if session.ProfilePath != "browser-sessions/profiles/marcus-aa" {
		t.Fatalf("session.ProfilePath = %q, want relative profile path", session.ProfilePath)
	}
	if session.LastVerifiedAt != nil || session.RevokedAt != nil {
		t.Fatalf("new session verification/revocation timestamps = %v/%v, want nil", session.LastVerifiedAt, session.RevokedAt)
	}
	if session.ExpiresAt == nil || !session.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("session.ExpiresAt = %v, want %v", session.ExpiresAt, expiresAt)
	}

	fetched, err := store.GetBrowserSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetBrowserSession() error = %v", err)
	}
	if fetched.ID != session.ID || fetched.Name != session.Name {
		t.Fatalf("fetched session = %+v, want %+v", fetched, session)
	}

	listed, err := store.ListBrowserSessions(ctx, ListBrowserSessionsParams{})
	if err != nil {
		t.Fatalf("ListBrowserSessions() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != session.ID {
		t.Fatalf("ListBrowserSessions() = %+v, want created session", listed)
	}

	store.Now = func() time.Time { return now.Add(time.Hour) }
	verified, err := store.UpdateBrowserSessionStatus(ctx, UpdateBrowserSessionStatusParams{
		SessionID: session.ID,
		Status:    BrowserSessionStatusVerified,
		Actor:     "operator",
		Reason:    "manual verification passed",
	})
	if err != nil {
		t.Fatalf("UpdateBrowserSessionStatus(verified) error = %v", err)
	}
	if verified.Status != BrowserSessionStatusVerified {
		t.Fatalf("verified.Status = %q, want %q", verified.Status, BrowserSessionStatusVerified)
	}
	if verified.LastVerifiedAt == nil || !verified.LastVerifiedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("verified.LastVerifiedAt = %v, want update time", verified.LastVerifiedAt)
	}

	verifiedList, err := store.ListBrowserSessions(ctx, ListBrowserSessionsParams{Status: BrowserSessionStatusVerified})
	if err != nil {
		t.Fatalf("ListBrowserSessions(verified) error = %v", err)
	}
	if len(verifiedList) != 1 || verifiedList[0].ID != session.ID {
		t.Fatalf("verified list = %+v, want verified session", verifiedList)
	}

	store.Now = func() time.Time { return now.Add(2 * time.Hour) }
	revoked, err := store.RevokeBrowserSession(ctx, RevokeBrowserSessionParams{
		SessionID: session.ID,
		Actor:     "operator",
		Reason:    "operator cleanup",
	})
	if err != nil {
		t.Fatalf("RevokeBrowserSession() error = %v", err)
	}
	if revoked.Status != BrowserSessionStatusRevoked {
		t.Fatalf("revoked.Status = %q, want %q", revoked.Status, BrowserSessionStatusRevoked)
	}
	if revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("revoked.RevokedAt = %v, want revoke time", revoked.RevokedAt)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		if event.StreamType == runtimeevents.StreamBrowserSession {
			counts[event.Type]++
			payload := string(event.Payload)
			for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
				if strings.Contains(strings.ToLower(payload), forbidden) {
					t.Fatalf("browser session audit payload contains forbidden token %q: %s", forbidden, payload)
				}
			}
		}
	}
	if counts[runtimeevents.EventBrowserSessionCreated] != 1 {
		t.Fatalf("browser.session_created events = %d, want 1", counts[runtimeevents.EventBrowserSessionCreated])
	}
	if counts[runtimeevents.EventBrowserSessionStatusChanged] != 1 {
		t.Fatalf("browser.session_status_changed events = %d, want 1", counts[runtimeevents.EventBrowserSessionStatusChanged])
	}
	if counts[runtimeevents.EventBrowserSessionRevoked] != 1 {
		t.Fatalf("browser.session_revoked events = %d, want 1", counts[runtimeevents.EventBrowserSessionRevoked])
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(reopened) error = %v", err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(reopened) error = %v", err)
	}
	persisted, err := reopened.GetBrowserSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetBrowserSession(reopened) error = %v", err)
	}
	if persisted.Status != BrowserSessionStatusRevoked {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, BrowserSessionStatusRevoked)
	}
}

func TestBrowserSessionProfilePathAllocationSanitizesAndRejectsUnsafePaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "browser-session-profile-paths.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Google Main!!",
		Domain:         "google.com",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession(default profile path) error = %v", err)
	}
	if session.ProfilePath != "browser-sessions/profiles/google-main" {
		t.Fatalf("session.ProfilePath = %q, want sanitized default under browser-sessions/profiles", session.ProfilePath)
	}

	explicit, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Explicit",
		Domain:         "example.com",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
		ProfilePath:    "browser-sessions/profiles/explicit-main",
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession(explicit safe path) error = %v", err)
	}
	if explicit.ProfilePath != "browser-sessions/profiles/explicit-main" {
		t.Fatalf("explicit.ProfilePath = %q, want cleaned explicit path", explicit.ProfilePath)
	}

	for _, unsafe := range []string{
		"../profiles/escape",
		"/tmp/odin-browser-profile",
		"browser-sessions/../profiles/escape",
		"state/cache/session",
		"browser-sessions/profiles/../../escape",
		"browser-sessions/profiles/nested/path",
	} {
		_, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
			Name:           "Unsafe",
			Domain:         "unsafe.example",
			PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
			ProfilePath:    unsafe,
		})
		if err == nil {
			t.Fatalf("CreateBrowserSession(profile_path=%q) error = nil, want rejection", unsafe)
		}
	}
}

func TestBrowserSessionProfileStoragePolicyDeniesWritesByDefault(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "browser-session-profile-storage-policy.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Google Main",
		Domain:         "google.com",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	if session.ProfileStoragePolicy != BrowserSessionProfileStoragePolicyEncryptedRequired {
		t.Fatalf("session.ProfileStoragePolicy = %q, want encrypted_required", session.ProfileStoragePolicy)
	}
	if CanWriteBrowserProfile(session) {
		t.Fatalf("CanWriteBrowserProfile(default policy) = true, want false")
	}

	prepared := session
	prepared.ProfileStoragePolicy = BrowserSessionProfileStoragePolicyPreparedUnencrypted
	if CanWriteBrowserProfile(prepared) {
		t.Fatalf("CanWriteBrowserProfile(prepared_unencrypted) = true, want false")
	}

	revoked := session
	revoked.Status = BrowserSessionStatusRevoked
	if CanWriteBrowserProfile(revoked) {
		t.Fatalf("CanWriteBrowserProfile(revoked) = true, want false")
	}

	reopened, err := store.GetBrowserSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetBrowserSession() error = %v", err)
	}
	if reopened.ProfileStoragePolicy != BrowserSessionProfileStoragePolicyEncryptedRequired {
		t.Fatalf("reopened.ProfileStoragePolicy = %q, want encrypted_required", reopened.ProfileStoragePolicy)
	}
}

func TestBrowserEncryptedProfileArtifactMetadataLifecyclePersistsAndAudits(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "browser-profile-artifacts.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	exists, err := store.HasTable(ctx, "browser_encrypted_profile_artifacts")
	if err != nil {
		t.Fatalf("HasTable(browser_encrypted_profile_artifacts) error = %v", err)
	}
	if !exists {
		t.Fatal("HasTable(browser_encrypted_profile_artifacts) = false, want true")
	}

	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Marcus Browser",
		Domain:         "example.com",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	expiresAt := now.Add(24 * time.Hour)
	artifact, err := store.CreateBrowserEncryptedProfileArtifact(ctx, CreateBrowserEncryptedProfileArtifactParams{
		SessionID:             session.ID,
		ProfilePath:           session.ProfilePath,
		EncryptedArtifactPath: "browser-sessions/encrypted-profiles/profile.enc",
		EncryptionKeyRef:      "local-key:v1",
		ExpiresAt:             &expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateBrowserEncryptedProfileArtifact() error = %v", err)
	}
	if artifact.ID <= 0 || artifact.SessionID != session.ID {
		t.Fatalf("artifact identity = %+v, want persisted session artifact", artifact)
	}
	if artifact.Status != BrowserEncryptedProfileArtifactStatusEncrypted {
		t.Fatalf("artifact.Status = %q, want %q", artifact.Status, BrowserEncryptedProfileArtifactStatusEncrypted)
	}
	if artifact.ProfilePath != session.ProfilePath || artifact.EncryptedArtifactPath != "browser-sessions/encrypted-profiles/profile.enc" {
		t.Fatalf("artifact paths = %q/%q, want session profile path and encrypted artifact path", artifact.ProfilePath, artifact.EncryptedArtifactPath)
	}
	if artifact.EncryptionKeyRef != "local-key:v1" {
		t.Fatalf("artifact.EncryptionKeyRef = %q, want key ref", artifact.EncryptionKeyRef)
	}
	if artifact.ExpiresAt == nil || !artifact.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("artifact.ExpiresAt = %v, want %v", artifact.ExpiresAt, expiresAt)
	}
	if artifact.RevokedAt != nil || artifact.CleanedAt != nil || artifact.ErrorCode != nil || artifact.ErrorMessage != nil {
		t.Fatalf("new artifact terminal fields = revoked_at:%v cleaned_at:%v error:%v/%v, want nil", artifact.RevokedAt, artifact.CleanedAt, artifact.ErrorCode, artifact.ErrorMessage)
	}

	fetched, err := store.GetBrowserEncryptedProfileArtifact(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("GetBrowserEncryptedProfileArtifact() error = %v", err)
	}
	if fetched.ID != artifact.ID || fetched.EncryptedArtifactPath != artifact.EncryptedArtifactPath {
		t.Fatalf("fetched artifact = %+v, want %+v", fetched, artifact)
	}

	listed, err := store.ListBrowserEncryptedProfileArtifacts(ctx, ListBrowserEncryptedProfileArtifactsParams{SessionID: session.ID})
	if err != nil {
		t.Fatalf("ListBrowserEncryptedProfileArtifacts() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != artifact.ID {
		t.Fatalf("listed artifacts = %+v, want created artifact", listed)
	}

	store.Now = func() time.Time { return now.Add(time.Hour) }
	revoked, err := store.MarkBrowserEncryptedProfileArtifactRevoked(ctx, MarkBrowserEncryptedProfileArtifactRevokedParams{
		ID:     artifact.ID,
		Actor:  "operator",
		Reason: "manual revocation",
	})
	if err != nil {
		t.Fatalf("MarkBrowserEncryptedProfileArtifactRevoked() error = %v", err)
	}
	if revoked.Status != BrowserEncryptedProfileArtifactStatusRevoked {
		t.Fatalf("revoked.Status = %q, want %q", revoked.Status, BrowserEncryptedProfileArtifactStatusRevoked)
	}
	if revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("revoked.RevokedAt = %v, want revoke time", revoked.RevokedAt)
	}
	store.Now = func() time.Time { return now.Add(90 * time.Minute) }
	cleaned, err := store.MarkBrowserEncryptedProfileArtifactCleaned(ctx, MarkBrowserEncryptedProfileArtifactCleanedParams{
		ID:     revoked.ID,
		Actor:  "retention",
		Reason: "encrypted artifact file removed",
	})
	if err != nil {
		t.Fatalf("MarkBrowserEncryptedProfileArtifactCleaned() error = %v", err)
	}
	if cleaned.Status != BrowserEncryptedProfileArtifactStatusCleaned {
		t.Fatalf("cleaned.Status = %q, want %q", cleaned.Status, BrowserEncryptedProfileArtifactStatusCleaned)
	}
	if cleaned.CleanedAt == nil || !cleaned.CleanedAt.Equal(now.Add(90*time.Minute)) {
		t.Fatalf("cleaned.CleanedAt = %v, want cleanup time", cleaned.CleanedAt)
	}

	store.Now = func() time.Time { return now.Add(2 * time.Hour) }
	expiring, err := store.CreateBrowserEncryptedProfileArtifact(ctx, CreateBrowserEncryptedProfileArtifactParams{
		SessionID:             session.ID,
		ProfilePath:           session.ProfilePath,
		EncryptedArtifactPath: "browser-sessions/encrypted-profiles/profile-2.enc",
		EncryptionKeyRef:      "local-key:v2",
	})
	if err != nil {
		t.Fatalf("CreateBrowserEncryptedProfileArtifact(expiring) error = %v", err)
	}
	expiredCode := "expired"
	expiredMessage := "profile artifact expired"
	expired, err := store.MarkBrowserEncryptedProfileArtifactExpired(ctx, MarkBrowserEncryptedProfileArtifactExpiredParams{
		ID:           expiring.ID,
		Actor:        "operator",
		Reason:       "expired by policy",
		ErrorCode:    &expiredCode,
		ErrorMessage: &expiredMessage,
	})
	if err != nil {
		t.Fatalf("MarkBrowserEncryptedProfileArtifactExpired() error = %v", err)
	}
	if expired.Status != BrowserEncryptedProfileArtifactStatusExpired {
		t.Fatalf("expired.Status = %q, want %q", expired.Status, BrowserEncryptedProfileArtifactStatusExpired)
	}
	if expired.ErrorCode == nil || *expired.ErrorCode != "expired" {
		t.Fatalf("expired.ErrorCode = %v, want expired", expired.ErrorCode)
	}
	cleanupCode := "cleanup_failed"
	cleanupMessage := "artifact path escaped ODIN_ROOT"
	failed, err := store.RecordBrowserEncryptedProfileArtifactCleanupFailed(ctx, RecordBrowserEncryptedProfileArtifactCleanupFailedParams{
		ID:           expired.ID,
		Actor:        "retention",
		Reason:       "cleanup failed",
		ErrorCode:    &cleanupCode,
		ErrorMessage: &cleanupMessage,
	})
	if err != nil {
		t.Fatalf("RecordBrowserEncryptedProfileArtifactCleanupFailed() error = %v", err)
	}
	if failed.Status != BrowserEncryptedProfileArtifactStatusExpired {
		t.Fatalf("failed.Status = %q, want expired status preserved", failed.Status)
	}
	if failed.ErrorCode == nil || *failed.ErrorCode != cleanupCode {
		t.Fatalf("failed.ErrorCode = %v, want cleanup_failed", failed.ErrorCode)
	}
	materialized, err := store.RecordBrowserProfileMaterialized(ctx, RecordBrowserProfileMaterializedParams{
		ID:                   expiring.ID,
		MaterializationPath:  "runtime/browser-profile-materializations/proof",
		MaterializedFilePath: "runtime/browser-profile-materializations/proof/profile.materialized",
		Actor:                "operator",
		Reason:               "read-only materialization",
	})
	if err != nil {
		t.Fatalf("RecordBrowserProfileMaterialized() error = %v", err)
	}
	if materialized.ID != expiring.ID || materialized.Status != BrowserEncryptedProfileArtifactStatusExpired {
		t.Fatalf("materialized artifact = %+v, want status-preserving audit", materialized)
	}
	materializationCleaned, err := store.RecordBrowserProfileMaterializationCleaned(ctx, RecordBrowserProfileMaterializationCleanedParams{
		ID:                  expiring.ID,
		MaterializationPath: "runtime/browser-profile-materializations/proof",
		Removed:             true,
		Actor:               "operator",
		Reason:              "read-only materialization cleanup",
	})
	if err != nil {
		t.Fatalf("RecordBrowserProfileMaterializationCleaned() error = %v", err)
	}
	if materializationCleaned.ID != expiring.ID || materializationCleaned.Status != BrowserEncryptedProfileArtifactStatusExpired {
		t.Fatalf("materializationCleaned artifact = %+v, want status-preserving audit", materializationCleaned)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		if event.StreamType != runtimeevents.StreamBrowserSession {
			continue
		}
		counts[event.Type]++
		payload := strings.ToLower(string(event.Payload))
		for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("encrypted profile artifact audit payload contains forbidden token %q: %s", forbidden, event.Payload)
			}
		}
	}
	if counts[runtimeevents.EventBrowserProfileEncrypted] != 2 {
		t.Fatalf("browser.profile_encrypted events = %d, want 2", counts[runtimeevents.EventBrowserProfileEncrypted])
	}
	if counts[runtimeevents.EventBrowserProfileRevoked] != 1 {
		t.Fatalf("browser.profile_revoked events = %d, want 1", counts[runtimeevents.EventBrowserProfileRevoked])
	}
	if counts[runtimeevents.EventBrowserProfileExpired] != 1 {
		t.Fatalf("browser.profile_expired events = %d, want 1", counts[runtimeevents.EventBrowserProfileExpired])
	}
	if counts[runtimeevents.EventBrowserProfileCleaned] != 1 {
		t.Fatalf("browser.profile_cleaned events = %d, want 1", counts[runtimeevents.EventBrowserProfileCleaned])
	}
	if counts[runtimeevents.EventBrowserProfileCleanupFailed] != 1 {
		t.Fatalf("browser.profile_cleanup_failed events = %d, want 1", counts[runtimeevents.EventBrowserProfileCleanupFailed])
	}
	if counts[runtimeevents.EventBrowserProfileMaterialized] != 1 {
		t.Fatalf("browser.profile_materialized events = %d, want 1", counts[runtimeevents.EventBrowserProfileMaterialized])
	}
	if counts[runtimeevents.EventBrowserProfileMaterializationCleaned] != 1 {
		t.Fatalf("browser.profile_materialization_cleaned events = %d, want 1", counts[runtimeevents.EventBrowserProfileMaterializationCleaned])
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(reopened) error = %v", err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(reopened) error = %v", err)
	}
	persisted, err := reopened.GetBrowserEncryptedProfileArtifact(ctx, revoked.ID)
	if err != nil {
		t.Fatalf("GetBrowserEncryptedProfileArtifact(reopened) error = %v", err)
	}
	if persisted.Status != BrowserEncryptedProfileArtifactStatusCleaned || persisted.CleanedAt == nil {
		t.Fatalf("persisted = %+v, want cleaned artifact with cleaned_at", persisted)
	}
}

func TestBrowserEncryptedProfileArtifactRejectsUnsafeMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "browser-profile-artifact-validation.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Safe Profile",
		Domain:         "example.com",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}

	for _, tc := range []struct {
		name   string
		params CreateBrowserEncryptedProfileArtifactParams
	}{
		{
			name: "absolute artifact path",
			params: CreateBrowserEncryptedProfileArtifactParams{
				SessionID:             session.ID,
				ProfilePath:           session.ProfilePath,
				EncryptedArtifactPath: "/tmp/profile.enc",
				EncryptionKeyRef:      "local-key:v1",
			},
		},
		{
			name: "artifact outside session profile path",
			params: CreateBrowserEncryptedProfileArtifactParams{
				SessionID:             session.ID,
				ProfilePath:           session.ProfilePath,
				EncryptedArtifactPath: "browser-sessions/profiles/other/profile.enc",
				EncryptionKeyRef:      "local-key:v1",
			},
		},
		{
			name: "profile path mismatch",
			params: CreateBrowserEncryptedProfileArtifactParams{
				SessionID:             session.ID,
				ProfilePath:           "browser-sessions/profiles/other",
				EncryptedArtifactPath: session.ProfilePath + "/profile.enc",
				EncryptionKeyRef:      "local-key:v1",
			},
		},
		{
			name: "missing encryption key ref",
			params: CreateBrowserEncryptedProfileArtifactParams{
				SessionID:             session.ID,
				ProfilePath:           session.ProfilePath,
				EncryptedArtifactPath: session.ProfilePath + "/profile.enc",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.CreateBrowserEncryptedProfileArtifact(ctx, tc.params); err == nil {
				t.Fatal("CreateBrowserEncryptedProfileArtifact() error = nil, want rejection")
			}
		})
	}

	rows, err := store.DB().QueryContext(ctx, `PRAGMA table_info(browser_encrypted_profile_artifacts)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(browser_encrypted_profile_artifacts) error = %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table_info error = %v", err)
		}
		lower := strings.ToLower(name)
		for _, forbidden := range []string{"password", "totp", "backup", "cookie", "credential"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("browser_encrypted_profile_artifacts column %q contains forbidden marker %q", name, forbidden)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows error = %v", err)
	}
}

func TestBrowserSessionMetadataRejectsCredentialLikeFields(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "browser-sessions-columns.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	rows, err := store.DB().QueryContext(ctx, `PRAGMA table_info(browser_session_profiles)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(browser_session_profiles) error = %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table_info error = %v", err)
		}
		lower := strings.ToLower(name)
		for _, forbidden := range []string{"password", "totp", "backup", "cookie", "token", "profile_bytes"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("browser_session_profiles column %q contains forbidden credential/profile byte field marker %q", name, forbidden)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows error = %v", err)
	}
}

func TestBrowserSessionLoginRequestLifecyclePersistsAndAudits(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "browser-session-login-requests.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	now := time.Date(2026, 5, 6, 19, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	exists, err := store.HasTable(ctx, "browser_session_login_requests")
	if err != nil {
		t.Fatalf("HasTable(browser_session_login_requests) error = %v", err)
	}
	if !exists {
		t.Fatal("HasTable(browser_session_login_requests) = false, want true")
	}

	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Google main",
		Domain:         "google.com",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
		ProfilePath:    "browser-sessions/profiles/google-main",
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}

	expiresAt := now.Add(10 * time.Minute)
	request, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest() error = %v", err)
	}
	if request.ID <= 0 || request.SessionID != session.ID {
		t.Fatalf("request = %+v, want persisted request for session %d", request, session.ID)
	}
	if request.Status != BrowserSessionLoginRequestStatusRequested {
		t.Fatalf("request.Status = %q, want %q", request.Status, BrowserSessionLoginRequestStatusRequested)
	}
	if request.HandoffURL != nil {
		t.Fatalf("request.HandoffURL = %v, want nil placeholder until handoff exists", request.HandoffURL)
	}
	if !request.ExpiresAt.Equal(expiresAt) || request.CompletedAt != nil {
		t.Fatalf("request timestamps = expires %v completed %v, want expires only", request.ExpiresAt, request.CompletedAt)
	}

	fetched, err := store.GetBrowserSessionLoginRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("GetBrowserSessionLoginRequest() error = %v", err)
	}
	if fetched.ID != request.ID || fetched.SessionID != session.ID {
		t.Fatalf("fetched request = %+v, want %+v", fetched, request)
	}

	listed, err := store.ListBrowserSessionLoginRequests(ctx, ListBrowserSessionLoginRequestsParams{SessionID: session.ID})
	if err != nil {
		t.Fatalf("ListBrowserSessionLoginRequests() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != request.ID {
		t.Fatalf("listed requests = %+v, want created request", listed)
	}

	store.Now = func() time.Time { return now.Add(time.Minute) }
	completed, err := store.CompleteBrowserSessionLoginRequest(ctx, CompleteBrowserSessionLoginRequestParams{
		RequestID: request.ID,
	})
	if err != nil {
		t.Fatalf("CompleteBrowserSessionLoginRequest() error = %v", err)
	}
	if completed.Status != BrowserSessionLoginRequestStatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed request = %+v, want completed with timestamp", completed)
	}

	store.Now = func() time.Time { return now.Add(2 * time.Minute) }
	expiring, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(12 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(expiring) error = %v", err)
	}
	store.Now = func() time.Time { return now.Add(3 * time.Minute) }
	expired, err := store.ExpireBrowserSessionLoginRequest(ctx, ExpireBrowserSessionLoginRequestParams{
		RequestID: expiring.ID,
	})
	if err != nil {
		t.Fatalf("ExpireBrowserSessionLoginRequest() error = %v", err)
	}
	if expired.Status != BrowserSessionLoginRequestStatusExpired || expired.CompletedAt != nil {
		t.Fatalf("expired request = %+v, want expired without completed timestamp", expired)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		if event.StreamType == runtimeevents.StreamBrowserSession {
			counts[event.Type]++
			payload := strings.ToLower(string(event.Payload))
			for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
				if strings.Contains(payload, forbidden) {
					t.Fatalf("browser session login request payload contains forbidden token %q: %s", forbidden, payload)
				}
			}
		}
	}
	if counts[runtimeevents.EventBrowserSessionLoginRequested] != 2 {
		t.Fatalf("browser.session_login_requested events = %d, want 2", counts[runtimeevents.EventBrowserSessionLoginRequested])
	}
	if counts[runtimeevents.EventBrowserSessionLoginCompleted] != 1 {
		t.Fatalf("browser.session_login_completed events = %d, want 1", counts[runtimeevents.EventBrowserSessionLoginCompleted])
	}
	if counts[runtimeevents.EventBrowserSessionLoginExpired] != 1 {
		t.Fatalf("browser.session_login_expired events = %d, want 1", counts[runtimeevents.EventBrowserSessionLoginExpired])
	}
}

func TestBrowserHandoffRunnerMetadataLifecyclePersistsAndAudits(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "browser-handoff-runners.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	now := time.Date(2026, 5, 6, 21, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	store.BrowserSessionHandoffID = func() (string, error) { return "opaque-runner-handoff", nil }

	exists, err := store.HasTable(ctx, "browser_handoff_runners")
	if err != nil {
		t.Fatalf("HasTable(browser_handoff_runners) error = %v", err)
	}
	if !exists {
		t.Fatal("HasTable(browser_handoff_runners) = false, want true")
	}

	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Research login",
		Domain:         "research.example",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	requestExpiresAt := now.Add(15 * time.Minute)
	request, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: requestExpiresAt,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest() error = %v", err)
	}

	bindAddr := "127.0.0.1:5901"
	privateBaseURL := "https://odin-handoff.tailnet.local"
	runner, err := store.CreateBrowserHandoffRunner(ctx, CreateBrowserHandoffRunnerParams{
		SessionID:      session.ID,
		LoginRequestID: request.ID,
		HandoffID:      request.HandoffID,
		BindAddr:       &bindAddr,
		PrivateBaseURL: &privateBaseURL,
		ExpiresAt:      now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserHandoffRunner() error = %v", err)
	}
	if runner.ID <= 0 || runner.SessionID != session.ID || runner.LoginRequestID != request.ID {
		t.Fatalf("runner = %+v, want persisted runner linked to session/request", runner)
	}
	if runner.Status != BrowserHandoffRunnerStatusRequested {
		t.Fatalf("runner.Status = %q, want %q", runner.Status, BrowserHandoffRunnerStatusRequested)
	}
	if runner.StartedAt != nil || runner.CompletedAt != nil || runner.CancelledAt != nil {
		t.Fatalf("new runner timestamps = started %v completed %v cancelled %v, want nil", runner.StartedAt, runner.CompletedAt, runner.CancelledAt)
	}
	if _, err := os.Stat(filepath.Join(root, "browser-sessions")); !os.IsNotExist(err) {
		t.Fatalf("browser handoff runner metadata created browser-sessions directory: err=%v", err)
	}

	fetched, err := store.GetBrowserHandoffRunner(ctx, runner.ID)
	if err != nil {
		t.Fatalf("GetBrowserHandoffRunner() error = %v", err)
	}
	if fetched.ID != runner.ID || fetched.HandoffID != request.HandoffID {
		t.Fatalf("fetched runner = %+v, want %+v", fetched, runner)
	}
	listed, err := store.ListBrowserHandoffRunners(ctx, ListBrowserHandoffRunnersParams{LoginRequestID: request.ID})
	if err != nil {
		t.Fatalf("ListBrowserHandoffRunners() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != runner.ID {
		t.Fatalf("listed runners = %+v, want created runner", listed)
	}

	store.Now = func() time.Time { return now.Add(time.Minute) }
	viewerURL := "https://odin-handoff.tailnet.local/session/browser-handoff-runner-1"
	externalRunnerID := "browser-handoff-runner-1"
	processID := int64(4242)
	started, err := store.UpdateBrowserHandoffRunnerStatus(ctx, UpdateBrowserHandoffRunnerStatusParams{
		ID:        runner.ID,
		Status:    BrowserHandoffRunnerStatusStarted,
		ViewerURL: &viewerURL,
		RunnerID:  &externalRunnerID,
		ProcessID: &processID,
		Actor:     "operator",
		Reason:    "handoff runner process started",
	})
	if err != nil {
		t.Fatalf("UpdateBrowserHandoffRunnerStatus(started) error = %v", err)
	}
	if started.Status != BrowserHandoffRunnerStatusStarted || started.StartedAt == nil {
		t.Fatalf("started runner = %+v, want started with timestamp", started)
	}
	if started.ViewerURL == nil || *started.ViewerURL != viewerURL || started.RunnerID == nil || *started.RunnerID != externalRunnerID || started.ProcessID == nil || *started.ProcessID != processID {
		t.Fatalf("started runner metadata = %+v, want viewer, runner id, and pid", started)
	}

	store.Now = func() time.Time { return now.Add(2 * time.Minute) }
	completed, err := store.UpdateBrowserHandoffRunnerStatus(ctx, UpdateBrowserHandoffRunnerStatusParams{
		ID:     runner.ID,
		Status: BrowserHandoffRunnerStatusCompleted,
		Actor:  "operator",
		Reason: "operator completed manual handoff",
	})
	if err != nil {
		t.Fatalf("UpdateBrowserHandoffRunnerStatus(completed) error = %v", err)
	}
	if completed.Status != BrowserHandoffRunnerStatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed runner = %+v, want completed with timestamp", completed)
	}
	if _, err := store.UpdateBrowserHandoffRunnerStatus(ctx, UpdateBrowserHandoffRunnerStatusParams{
		ID:     runner.ID,
		Status: BrowserHandoffRunnerStatusStarted,
	}); err == nil {
		t.Fatal("UpdateBrowserHandoffRunnerStatus(completed -> started) error = nil, want rejection")
	}

	expiring, err := store.CreateBrowserHandoffRunner(ctx, CreateBrowserHandoffRunnerParams{
		SessionID:      session.ID,
		LoginRequestID: request.ID,
		HandoffID:      request.HandoffID,
		ExpiresAt:      now.Add(11 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserHandoffRunner(expiring) error = %v", err)
	}
	store.Now = func() time.Time { return now.Add(3 * time.Minute) }
	expired, err := store.ExpireBrowserHandoffRunner(ctx, ExpireBrowserHandoffRunnerParams{
		ID:     expiring.ID,
		Actor:  "operator",
		Reason: "handoff runner timed out",
	})
	if err != nil {
		t.Fatalf("ExpireBrowserHandoffRunner() error = %v", err)
	}
	if expired.Status != BrowserHandoffRunnerStatusExpired {
		t.Fatalf("expired.Status = %q, want %q", expired.Status, BrowserHandoffRunnerStatusExpired)
	}

	cancelling, err := store.CreateBrowserHandoffRunner(ctx, CreateBrowserHandoffRunnerParams{
		SessionID:      session.ID,
		LoginRequestID: request.ID,
		HandoffID:      request.HandoffID,
		ExpiresAt:      now.Add(12 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserHandoffRunner(cancelling) error = %v", err)
	}
	store.Now = func() time.Time { return now.Add(4 * time.Minute) }
	cancelled, err := store.CancelBrowserHandoffRunner(ctx, CancelBrowserHandoffRunnerParams{
		ID:     cancelling.ID,
		Actor:  "operator",
		Reason: "operator cancelled handoff",
	})
	if err != nil {
		t.Fatalf("CancelBrowserHandoffRunner() error = %v", err)
	}
	if cancelled.Status != BrowserHandoffRunnerStatusCancelled || cancelled.CancelledAt == nil {
		t.Fatalf("cancelled runner = %+v, want cancelled with timestamp", cancelled)
	}

	failing, err := store.CreateBrowserHandoffRunner(ctx, CreateBrowserHandoffRunnerParams{
		SessionID:      session.ID,
		LoginRequestID: request.ID,
		HandoffID:      request.HandoffID,
		ExpiresAt:      now.Add(13 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserHandoffRunner(failing) error = %v", err)
	}
	store.Now = func() time.Time { return now.Add(5 * time.Minute) }
	errorCode := "novnc_unavailable"
	errorMessage := "runner failed before viewer became available"
	failed, err := store.UpdateBrowserHandoffRunnerStatus(ctx, UpdateBrowserHandoffRunnerStatusParams{
		ID:           failing.ID,
		Status:       BrowserHandoffRunnerStatusFailed,
		ErrorCode:    &errorCode,
		ErrorMessage: &errorMessage,
		Actor:        "runner",
		Reason:       "runner process failed",
	})
	if err != nil {
		t.Fatalf("UpdateBrowserHandoffRunnerStatus(failed) error = %v", err)
	}
	if failed.Status != BrowserHandoffRunnerStatusFailed || failed.ErrorCode == nil || *failed.ErrorCode != errorCode {
		t.Fatalf("failed runner = %+v, want failed with error code", failed)
	}

	if _, err := store.CreateBrowserHandoffRunner(ctx, CreateBrowserHandoffRunnerParams{
		SessionID:      session.ID,
		LoginRequestID: request.ID,
		HandoffID:      request.HandoffID,
		ExpiresAt:      requestExpiresAt.Add(time.Minute),
	}); err == nil {
		t.Fatal("CreateBrowserHandoffRunner(expires after login request) error = nil, want rejection")
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		if event.StreamType != runtimeevents.StreamBrowserSession {
			continue
		}
		counts[event.Type]++
		payload := strings.ToLower(string(event.Payload))
		for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("browser handoff runner audit payload contains forbidden token %q: %s", forbidden, payload)
			}
		}
	}
	if counts[runtimeevents.EventBrowserHandoffRunnerRequested] != 4 {
		t.Fatalf("browser.handoff_runner_requested events = %d, want 4", counts[runtimeevents.EventBrowserHandoffRunnerRequested])
	}
	if counts[runtimeevents.EventBrowserHandoffRunnerStarted] != 1 {
		t.Fatalf("browser.handoff_runner_started events = %d, want 1", counts[runtimeevents.EventBrowserHandoffRunnerStarted])
	}
	if counts[runtimeevents.EventBrowserHandoffRunnerCompleted] != 1 {
		t.Fatalf("browser.handoff_runner_completed events = %d, want 1", counts[runtimeevents.EventBrowserHandoffRunnerCompleted])
	}
	if counts[runtimeevents.EventBrowserHandoffRunnerExpired] != 1 {
		t.Fatalf("browser.handoff_runner_expired events = %d, want 1", counts[runtimeevents.EventBrowserHandoffRunnerExpired])
	}
	if counts[runtimeevents.EventBrowserHandoffRunnerCancelled] != 1 {
		t.Fatalf("browser.handoff_runner_cancelled events = %d, want 1", counts[runtimeevents.EventBrowserHandoffRunnerCancelled])
	}
	if counts[runtimeevents.EventBrowserHandoffRunnerFailed] != 1 {
		t.Fatalf("browser.handoff_runner_failed events = %d, want 1", counts[runtimeevents.EventBrowserHandoffRunnerFailed])
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(reopened) error = %v", err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(reopened) error = %v", err)
	}
	persisted, err := reopened.GetBrowserHandoffRunner(ctx, runner.ID)
	if err != nil {
		t.Fatalf("GetBrowserHandoffRunner(reopened) error = %v", err)
	}
	if persisted.Status != BrowserHandoffRunnerStatusCompleted {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, BrowserHandoffRunnerStatusCompleted)
	}
}

func TestBrowserSessionLoginRequestHandoffMetadataIsOpaqueAndValidated(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "browser-session-login-handoff.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	now := time.Date(2026, 5, 6, 20, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	store.BrowserSessionHandoffID = func() (string, error) { return "opaque-handoff-id", nil }

	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Google main",
		Domain:         "google.com",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}

	expiresAt := now.Add(10 * time.Minute)
	request, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID:      session.ID,
		HandoffBaseURL: "https://odin-handoff.tailnet.local/manual-login",
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(with base URL) error = %v", err)
	}
	if request.HandoffID != "opaque-handoff-id" {
		t.Fatalf("request.HandoffID = %q, want injected opaque id", request.HandoffID)
	}
	sessionIDString := strconv.FormatInt(session.ID, 10)
	if strings.Contains(request.HandoffID, sessionIDString) {
		t.Fatalf("request.HandoffID = %q, must not expose session id %d", request.HandoffID, session.ID)
	}
	if request.HandoffURL == nil || *request.HandoffURL != "https://odin-handoff.tailnet.local/manual-login?handoff_id=opaque-handoff-id" {
		t.Fatalf("request.HandoffURL = %v, want metadata URL with opaque id", request.HandoffURL)
	}

	persisted, err := store.GetBrowserSessionLoginRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("GetBrowserSessionLoginRequest() error = %v", err)
	}
	if persisted.HandoffID != request.HandoffID || persisted.HandoffURL == nil || *persisted.HandoffURL != *request.HandoffURL {
		t.Fatalf("persisted request = %+v, want handoff metadata persisted", persisted)
	}

	withoutBase, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(11 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(no base URL) error = %v", err)
	}
	if withoutBase.HandoffID == "" || withoutBase.HandoffURL != nil {
		t.Fatalf("withoutBase = %+v, want opaque id with nil URL when no base URL", withoutBase)
	}

	for _, unsafeBaseURL := range []string{"ssh://odin-handoff.tailnet.local/manual-login", "https://", "not-a-url"} {
		_, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
			SessionID:      session.ID,
			HandoffBaseURL: unsafeBaseURL,
			ExpiresAt:      now.Add(12 * time.Minute),
		})
		if err == nil {
			t.Fatalf("CreateBrowserSessionLoginRequest(base=%q) error = nil, want rejection", unsafeBaseURL)
		}
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var sawRequested bool
	for _, event := range events {
		if event.Type != runtimeevents.EventBrowserSessionLoginRequested {
			continue
		}
		payload := string(event.Payload)
		if strings.Contains(payload, `"session_id":"`) || strings.Contains(payload, `"handoff_id":"`+sessionIDString+`"`) {
			t.Fatalf("login request event payload exposes session id as string token: %s", payload)
		}
		if strings.Contains(payload, `"handoff_id":"opaque-handoff-id"`) {
			sawRequested = true
		}
	}
	if !sawRequested {
		t.Fatalf("events = %+v, want browser.session_login_requested with handoff_id", events)
	}
}

func TestBrowserSessionLoginHandoffLookupValidatesRequestAndSession(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "browser-session-login-handoff-lookup.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	now := time.Date(2026, 5, 6, 20, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Google main",
		Domain:         "google.com",
		AccountHint:    "marcus",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}

	handoffIDs := []string{"valid-handoff", "completed-handoff", "expired-status-handoff", "cancelled-handoff", "expired-time-handoff"}
	store.BrowserSessionHandoffID = func() (string, error) {
		if len(handoffIDs) == 0 {
			t.Fatal("unexpected handoff id request")
		}
		next := handoffIDs[0]
		handoffIDs = handoffIDs[1:]
		return next, nil
	}

	validRequest, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(valid) error = %v", err)
	}
	handoff, err := store.GetBrowserSessionLoginHandoff(ctx, "valid-handoff")
	if err != nil {
		t.Fatalf("GetBrowserSessionLoginHandoff(valid) error = %v", err)
	}
	if handoff.HandoffID != "valid-handoff" || handoff.LoginRequest.ID != validRequest.ID || handoff.Session.ID != session.ID || handoff.Session.AccountHint != "marcus" {
		t.Fatalf("handoff = %+v, want linked request and session metadata", handoff)
	}

	completed, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(completed) error = %v", err)
	}
	if _, err := store.CompleteBrowserSessionLoginRequest(ctx, CompleteBrowserSessionLoginRequestParams{RequestID: completed.ID}); err != nil {
		t.Fatalf("CompleteBrowserSessionLoginRequest() error = %v", err)
	}
	if _, err := store.GetBrowserSessionLoginHandoff(ctx, "completed-handoff"); err == nil || !strings.Contains(err.Error(), "status \"completed\"") {
		t.Fatalf("GetBrowserSessionLoginHandoff(completed) error = %v, want status rejection", err)
	}

	expiredStatus, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(expired status) error = %v", err)
	}
	if _, err := store.ExpireBrowserSessionLoginRequest(ctx, ExpireBrowserSessionLoginRequestParams{RequestID: expiredStatus.ID}); err != nil {
		t.Fatalf("ExpireBrowserSessionLoginRequest() error = %v", err)
	}
	if _, err := store.GetBrowserSessionLoginHandoff(ctx, "expired-status-handoff"); err == nil || !strings.Contains(err.Error(), "status \"expired\"") {
		t.Fatalf("GetBrowserSessionLoginHandoff(expired status) error = %v, want status rejection", err)
	}

	cancelled, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(cancelled) error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE browser_session_login_requests SET status = ? WHERE id = ?`, string(BrowserSessionLoginRequestStatusCancelled), cancelled.ID); err != nil {
		t.Fatalf("cancel login request error = %v", err)
	}
	if _, err := store.GetBrowserSessionLoginHandoff(ctx, "cancelled-handoff"); err == nil || !strings.Contains(err.Error(), "status \"cancelled\"") {
		t.Fatalf("GetBrowserSessionLoginHandoff(cancelled) error = %v, want status rejection", err)
	}

	if _, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(expired time) error = %v", err)
	}
	store.Now = func() time.Time { return now.Add(11 * time.Minute) }
	if _, err := store.GetBrowserSessionLoginHandoff(ctx, "expired-time-handoff"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("GetBrowserSessionLoginHandoff(expired time) error = %v, want expiration rejection", err)
	}

	if _, err := store.GetBrowserSessionLoginHandoff(ctx, "missing-handoff"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("GetBrowserSessionLoginHandoff(missing) error = %v, want not found", err)
	}
	if _, err := store.GetBrowserSessionLoginHandoff(ctx, " "); err == nil || !strings.Contains(err.Error(), "handoff id is required") {
		t.Fatalf("GetBrowserSessionLoginHandoff(empty) error = %v, want required id", err)
	}

	revokedSession, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "GitHub main",
		Domain:         "github.com",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession(revoked) error = %v", err)
	}
	store.Now = func() time.Time { return now }
	store.BrowserSessionHandoffID = func() (string, error) { return "revoked-handoff", nil }
	if _, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: revokedSession.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(revoked session) error = %v", err)
	}
	if _, err := store.RevokeBrowserSession(ctx, RevokeBrowserSessionParams{
		SessionID: revokedSession.ID,
		Actor:     "operator",
		Reason:    "test revocation",
	}); err != nil {
		t.Fatalf("RevokeBrowserSession() error = %v", err)
	}
	if _, err := store.GetBrowserSessionLoginHandoff(ctx, "revoked-handoff"); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("GetBrowserSessionLoginHandoff(revoked session) error = %v, want revoked rejection", err)
	}
}

func TestBrowserSessionManualVerificationCompletesLoginRequestAndAudits(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "browser-session-verify.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	now := time.Date(2026, 5, 6, 20, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Google main",
		Domain:         "google.com",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
		ProfilePath:    "browser-sessions/profiles/google-main",
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	request, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest() error = %v", err)
	}

	verifiedAt := now.Add(2 * time.Minute)
	store.Now = func() time.Time { return verifiedAt }
	verified, completed, err := store.VerifyBrowserSession(ctx, VerifyBrowserSessionParams{
		SessionID:      session.ID,
		LoginRequestID: request.ID,
		Actor:          "operator",
		Reason:         "manual login completed",
	})
	if err != nil {
		t.Fatalf("VerifyBrowserSession() error = %v", err)
	}
	if verified.Status != BrowserSessionStatusVerified {
		t.Fatalf("verified.Status = %q, want %q", verified.Status, BrowserSessionStatusVerified)
	}
	if verified.LastVerifiedAt == nil || !verified.LastVerifiedAt.Equal(verifiedAt) {
		t.Fatalf("verified.LastVerifiedAt = %v, want %v", verified.LastVerifiedAt, verifiedAt)
	}
	if completed == nil || completed.ID != request.ID || completed.Status != BrowserSessionLoginRequestStatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed request = %+v, want completed login request", completed)
	}
	persistedRequest, err := store.GetBrowserSessionLoginRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("GetBrowserSessionLoginRequest() error = %v", err)
	}
	if persistedRequest.Status != BrowserSessionLoginRequestStatusCompleted || persistedRequest.CompletedAt == nil {
		t.Fatalf("persisted request = %+v, want completed", persistedRequest)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		if event.StreamType == runtimeevents.StreamBrowserSession {
			counts[event.Type]++
			payload := strings.ToLower(string(event.Payload))
			for _, forbidden := range []string{"password", "totp", "backup_code", "cookie", "profile_bytes"} {
				if strings.Contains(payload, forbidden) {
					t.Fatalf("browser session verification payload contains forbidden token %q: %s", forbidden, payload)
				}
			}
		}
	}
	if counts[runtimeevents.EventBrowserSessionStatusChanged] != 1 {
		t.Fatalf("browser.session_status_changed events = %d, want 1", counts[runtimeevents.EventBrowserSessionStatusChanged])
	}
	if counts[runtimeevents.EventBrowserSessionVerified] != 1 {
		t.Fatalf("browser.session_verified events = %d, want 1", counts[runtimeevents.EventBrowserSessionVerified])
	}
	if counts[runtimeevents.EventBrowserSessionLoginCompleted] != 1 {
		t.Fatalf("browser.session_login_completed events = %d, want 1", counts[runtimeevents.EventBrowserSessionLoginCompleted])
	}
}

func TestBrowserSessionManualVerificationRejectsRevokedAndInvalidLoginRequests(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "browser-session-verify-rejections.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	now := time.Date(2026, 5, 6, 21, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	session, err := store.CreateBrowserSession(ctx, CreateBrowserSessionParams{
		Name:           "Google main",
		Domain:         "google.com",
		PermissionTier: BrowserSessionPermissionTierAuthenticatedReadOnly,
		ProfilePath:    "browser-sessions/profiles/google-main",
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	expiring, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest() error = %v", err)
	}
	expired, err := store.ExpireBrowserSessionLoginRequest(ctx, ExpireBrowserSessionLoginRequestParams{RequestID: expiring.ID})
	if err != nil {
		t.Fatalf("ExpireBrowserSessionLoginRequest() error = %v", err)
	}
	_, _, err = store.VerifyBrowserSession(ctx, VerifyBrowserSessionParams{
		SessionID:      session.ID,
		LoginRequestID: expired.ID,
		Actor:          "operator",
		Reason:         "manual login completed",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot complete") {
		t.Fatalf("VerifyBrowserSession(expired request) error = %v, want cannot complete", err)
	}

	cancelled, err := store.CreateBrowserSessionLoginRequest(ctx, CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(cancelled) error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE browser_session_login_requests SET status = ? WHERE id = ?`, string(BrowserSessionLoginRequestStatusCancelled), cancelled.ID); err != nil {
		t.Fatalf("set cancelled login request error = %v", err)
	}
	_, _, err = store.VerifyBrowserSession(ctx, VerifyBrowserSessionParams{
		SessionID:      session.ID,
		LoginRequestID: cancelled.ID,
		Actor:          "operator",
		Reason:         "manual login completed",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot complete") {
		t.Fatalf("VerifyBrowserSession(cancelled request) error = %v, want cannot complete", err)
	}

	if _, err := store.RevokeBrowserSession(ctx, RevokeBrowserSessionParams{
		SessionID: session.ID,
		Actor:     "operator",
		Reason:    "test revocation",
	}); err != nil {
		t.Fatalf("RevokeBrowserSession() error = %v", err)
	}
	_, _, err = store.VerifyBrowserSession(ctx, VerifyBrowserSessionParams{
		SessionID: session.ID,
		Actor:     "operator",
		Reason:    "manual login completed",
	})
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("VerifyBrowserSession(revoked session) error = %v, want revoked rejection", err)
	}
}
