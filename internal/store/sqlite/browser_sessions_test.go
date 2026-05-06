package sqlite

import (
	"context"
	"path/filepath"
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
		ProfilePath:    "browser-sessions/profiles/marcus-aa",
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
