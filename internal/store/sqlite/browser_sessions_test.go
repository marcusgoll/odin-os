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
