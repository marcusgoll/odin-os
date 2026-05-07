package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
)

type BrowserSessionStatus string
type BrowserSessionPermissionTier string
type BrowserSessionProfileStoragePolicy string
type BrowserSessionLoginRequestStatus string
type BrowserHandoffRunnerStatus string

const (
	BrowserSessionStatusCreated        BrowserSessionStatus = "created"
	BrowserSessionStatusLoginRequested BrowserSessionStatus = "login_requested"
	BrowserSessionStatusVerified       BrowserSessionStatus = "verified"
	BrowserSessionStatusExpired        BrowserSessionStatus = "expired"
	BrowserSessionStatusRevoked        BrowserSessionStatus = "revoked"
)

const (
	BrowserSessionPermissionTierPublicReadOnly        BrowserSessionPermissionTier = "public_readonly"
	BrowserSessionPermissionTierAuthenticatedReadOnly BrowserSessionPermissionTier = "authenticated_readonly"
)

const (
	BrowserSessionProfileStoragePolicyDisabled            BrowserSessionProfileStoragePolicy = "disabled"
	BrowserSessionProfileStoragePolicyPreparedUnencrypted BrowserSessionProfileStoragePolicy = "prepared_unencrypted"
	BrowserSessionProfileStoragePolicyEncryptedRequired   BrowserSessionProfileStoragePolicy = "encrypted_required"
)

const (
	BrowserSessionLoginRequestStatusRequested BrowserSessionLoginRequestStatus = "requested"
	BrowserSessionLoginRequestStatusCompleted BrowserSessionLoginRequestStatus = "completed"
	BrowserSessionLoginRequestStatusExpired   BrowserSessionLoginRequestStatus = "expired"
	BrowserSessionLoginRequestStatusCancelled BrowserSessionLoginRequestStatus = "cancelled"
)

const (
	BrowserHandoffRunnerStatusRequested BrowserHandoffRunnerStatus = "requested"
	BrowserHandoffRunnerStatusStarted   BrowserHandoffRunnerStatus = "started"
	BrowserHandoffRunnerStatusCompleted BrowserHandoffRunnerStatus = "completed"
	BrowserHandoffRunnerStatusExpired   BrowserHandoffRunnerStatus = "expired"
	BrowserHandoffRunnerStatusCancelled BrowserHandoffRunnerStatus = "cancelled"
	BrowserHandoffRunnerStatusFailed    BrowserHandoffRunnerStatus = "failed"
)

type BrowserSession struct {
	ID                   int64
	Name                 string
	Domain               string
	AccountHint          string
	PermissionTier       BrowserSessionPermissionTier
	Status               BrowserSessionStatus
	ProfileStoragePolicy BrowserSessionProfileStoragePolicy
	ProfilePath          string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	LastVerifiedAt       *time.Time
	ExpiresAt            *time.Time
	RevokedAt            *time.Time
}

type BrowserSessionLoginRequest struct {
	ID          int64
	SessionID   int64
	Status      BrowserSessionLoginRequestStatus
	HandoffID   string
	HandoffURL  *string
	ExpiresAt   time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type BrowserSessionLoginHandoff struct {
	HandoffID    string
	LoginRequest BrowserSessionLoginRequest
	Session      BrowserSession
}

type BrowserHandoffRunner struct {
	ID             int64
	SessionID      int64
	LoginRequestID int64
	HandoffID      string
	Status         BrowserHandoffRunnerStatus
	ViewerURL      *string
	RunnerID       *string
	ProcessID      *int64
	BindAddr       *string
	PrivateBaseURL *string
	PublicBaseURL  *string
	ExpiresAt      time.Time
	StartedAt      *time.Time
	ExitedAt       *time.Time
	CompletedAt    *time.Time
	CancelledAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ErrorCode      *string
	ErrorMessage   *string
}

type CreateBrowserSessionParams struct {
	Name           string
	Domain         string
	AccountHint    string
	PermissionTier BrowserSessionPermissionTier
	ProfilePath    string
	ExpiresAt      *time.Time
}

type ListBrowserSessionsParams struct {
	Status BrowserSessionStatus
	Domain string
	Limit  int
}

type UpdateBrowserSessionStatusParams struct {
	SessionID int64
	Status    BrowserSessionStatus
	Actor     string
	Reason    string
}

type RevokeBrowserSessionParams struct {
	SessionID int64
	Actor     string
	Reason    string
}

type VerifyBrowserSessionParams struct {
	SessionID      int64
	LoginRequestID int64
	Actor          string
	Reason         string
}

type RecordBrowserSessionProfilePreparedParams struct {
	SessionID   int64
	ProfilePath string
	Created     bool
	Actor       string
}

type CreateBrowserSessionLoginRequestParams struct {
	SessionID      int64
	HandoffBaseURL string
	ExpiresAt      time.Time
}

type ListBrowserSessionLoginRequestsParams struct {
	SessionID int64
}

type CompleteBrowserSessionLoginRequestParams struct {
	RequestID int64
}

type ExpireBrowserSessionLoginRequestParams struct {
	RequestID int64
}

type CreateBrowserHandoffRunnerParams struct {
	SessionID      int64
	LoginRequestID int64
	HandoffID      string
	ViewerURL      *string
	RunnerID       *string
	ProcessID      *int64
	BindAddr       *string
	PrivateBaseURL *string
	PublicBaseURL  *string
	ExpiresAt      time.Time
}

type ListBrowserHandoffRunnersParams struct {
	LoginRequestID int64
}

type UpdateBrowserHandoffRunnerStatusParams struct {
	ID             int64
	Status         BrowserHandoffRunnerStatus
	ViewerURL      *string
	RunnerID       *string
	ProcessID      *int64
	BindAddr       *string
	PrivateBaseURL *string
	PublicBaseURL  *string
	ErrorCode      *string
	ErrorMessage   *string
	Actor          string
	Reason         string
}

type ExpireBrowserHandoffRunnerParams struct {
	ID     int64
	Actor  string
	Reason string
}

type CancelBrowserHandoffRunnerParams struct {
	ID     int64
	Actor  string
	Reason string
}

func (store *Store) CreateBrowserSession(ctx context.Context, params CreateBrowserSessionParams) (BrowserSession, error) {
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return BrowserSession{}, fmt.Errorf("browser session name is required")
	}
	domain, err := normalizeBrowserSessionDomain(params.Domain)
	if err != nil {
		return BrowserSession{}, err
	}
	tier := normalizeBrowserSessionPermissionTier(params.PermissionTier)
	if tier == "" {
		return BrowserSession{}, fmt.Errorf("browser session permission tier is required")
	}
	profilePath, err := normalizeBrowserSessionProfilePath(params.ProfilePath, name)
	if err != nil {
		return BrowserSession{}, err
	}
	now := store.now()
	session := BrowserSession{
		Name:                 name,
		Domain:               domain,
		AccountHint:          strings.TrimSpace(params.AccountHint),
		PermissionTier:       tier,
		Status:               BrowserSessionStatusCreated,
		ProfileStoragePolicy: BrowserSessionProfileStoragePolicyEncryptedRequired,
		ProfilePath:          profilePath,
		CreatedAt:            now,
		UpdatedAt:            now,
		ExpiresAt:            cloneTimePtr(params.ExpiresAt),
	}

	err = store.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO browser_session_profiles (
				name, domain, account_hint, permission_tier, status, profile_storage_policy, profile_path,
				created_at, updated_at, last_verified_at, expires_at, revoked_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, NULL)
		`,
			session.Name,
			session.Domain,
			session.AccountHint,
			string(session.PermissionTier),
			string(session.Status),
			string(session.ProfileStoragePolicy),
			session.ProfilePath,
			formatTime(now),
			formatTime(now),
			nullTime(session.ExpiresAt),
		)
		if err != nil {
			return err
		}
		session.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		return appendBrowserSessionEventTx(ctx, tx, session, runtimeevents.EventBrowserSessionCreated, runtimeevents.BrowserSessionCreatedPayload{
			SessionID:            session.ID,
			Name:                 session.Name,
			Domain:               session.Domain,
			AccountHint:          session.AccountHint,
			PermissionTier:       string(session.PermissionTier),
			Status:               string(session.Status),
			ProfileStoragePolicy: string(session.ProfileStoragePolicy),
			ProfilePath:          session.ProfilePath,
			ExpiresAt:            formatOptionalTime(session.ExpiresAt),
		}, now)
	})
	return session, err
}

func (store *Store) GetBrowserSession(ctx context.Context, id int64) (BrowserSession, error) {
	if id <= 0 {
		return BrowserSession{}, fmt.Errorf("browser session id must be positive")
	}
	row := store.db.QueryRowContext(ctx, browserSessionSelectSQL()+` WHERE id = ?`, id)
	return scanBrowserSession(row)
}

func (store *Store) ListBrowserSessions(ctx context.Context, params ListBrowserSessionsParams) ([]BrowserSession, error) {
	query := browserSessionSelectSQL()
	var args []any
	clauses := []string{}
	if params.Status != "" {
		status := normalizeBrowserSessionStatus(params.Status)
		if status == "" {
			return nil, fmt.Errorf("unsupported browser session status: %s", params.Status)
		}
		clauses = append(clauses, `status = ?`)
		args = append(args, string(status))
	}
	if strings.TrimSpace(params.Domain) != "" {
		domain, err := normalizeBrowserSessionDomain(params.Domain)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, `domain = ?`)
		args = append(args, domain)
	}
	if len(clauses) > 0 {
		query += ` WHERE ` + strings.Join(clauses, ` AND `)
	}
	query += ` ORDER BY id ASC`
	if params.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, params.Limit)
	}
	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := make([]BrowserSession, 0)
	for rows.Next() {
		session, err := scanBrowserSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (store *Store) UpdateBrowserSessionStatus(ctx context.Context, params UpdateBrowserSessionStatusParams) (BrowserSession, error) {
	if params.SessionID <= 0 {
		return BrowserSession{}, fmt.Errorf("browser session id must be positive")
	}
	status := normalizeBrowserSessionStatus(params.Status)
	if status == "" {
		return BrowserSession{}, fmt.Errorf("browser session status is required")
	}
	if status == BrowserSessionStatusRevoked {
		return BrowserSession{}, fmt.Errorf("use RevokeBrowserSession for revoked status")
	}
	now := store.now()
	var updated BrowserSession
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := getBrowserSessionTx(ctx, tx, params.SessionID)
		if err != nil {
			return err
		}
		if current.Status == BrowserSessionStatusRevoked {
			return fmt.Errorf("revoked browser session cannot change status")
		}
		if current.Status == status {
			updated = current
			return nil
		}
		lastVerifiedAt := current.LastVerifiedAt
		if status == BrowserSessionStatusVerified {
			lastVerifiedAt = &now
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE browser_session_profiles
			SET status = ?, updated_at = ?, last_verified_at = ?
			WHERE id = ?
		`, string(status), formatTime(now), nullTime(lastVerifiedAt), current.ID); err != nil {
			return err
		}
		updated = current
		updated.Status = status
		updated.UpdatedAt = now
		updated.LastVerifiedAt = cloneTimePtr(lastVerifiedAt)
		return appendBrowserSessionEventTx(ctx, tx, updated, runtimeevents.EventBrowserSessionStatusChanged, runtimeevents.BrowserSessionStatusChangedPayload{
			SessionID:      updated.ID,
			PreviousStatus: string(current.Status),
			Status:         string(status),
			Actor:          defaultString(params.Actor, "operator"),
			Reason:         strings.TrimSpace(params.Reason),
			LastVerifiedAt: formatOptionalTime(updated.LastVerifiedAt),
			ExpiresAt:      formatOptionalTime(updated.ExpiresAt),
		}, now)
	})
	return updated, err
}

func (store *Store) RevokeBrowserSession(ctx context.Context, params RevokeBrowserSessionParams) (BrowserSession, error) {
	if params.SessionID <= 0 {
		return BrowserSession{}, fmt.Errorf("browser session id must be positive")
	}
	reason := strings.TrimSpace(params.Reason)
	if reason == "" {
		return BrowserSession{}, fmt.Errorf("browser session revoke reason is required")
	}
	now := store.now()
	var revoked BrowserSession
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := getBrowserSessionTx(ctx, tx, params.SessionID)
		if err != nil {
			return err
		}
		if current.Status == BrowserSessionStatusRevoked {
			revoked = current
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE browser_session_profiles
			SET status = ?, updated_at = ?, revoked_at = ?
			WHERE id = ?
		`, string(BrowserSessionStatusRevoked), formatTime(now), formatTime(now), current.ID); err != nil {
			return err
		}
		revoked = current
		revoked.Status = BrowserSessionStatusRevoked
		revoked.UpdatedAt = now
		revoked.RevokedAt = &now
		return appendBrowserSessionEventTx(ctx, tx, revoked, runtimeevents.EventBrowserSessionRevoked, runtimeevents.BrowserSessionRevokedPayload{
			SessionID:      revoked.ID,
			PreviousStatus: string(current.Status),
			Status:         string(BrowserSessionStatusRevoked),
			Actor:          defaultString(params.Actor, "operator"),
			Reason:         reason,
			RevokedAt:      formatTime(now),
		}, now)
	})
	return revoked, err
}

func (store *Store) VerifyBrowserSession(ctx context.Context, params VerifyBrowserSessionParams) (BrowserSession, *BrowserSessionLoginRequest, error) {
	if params.SessionID <= 0 {
		return BrowserSession{}, nil, fmt.Errorf("browser session id must be positive")
	}
	if params.LoginRequestID < 0 {
		return BrowserSession{}, nil, fmt.Errorf("browser session login request id must be positive")
	}
	now := store.now()
	var verified BrowserSession
	var completed *BrowserSessionLoginRequest
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := getBrowserSessionTx(ctx, tx, params.SessionID)
		if err != nil {
			return err
		}
		if current.Status == BrowserSessionStatusRevoked {
			return fmt.Errorf("revoked browser session cannot be verified")
		}
		var request *BrowserSessionLoginRequest
		if params.LoginRequestID > 0 {
			currentRequest, err := getBrowserSessionLoginRequestTx(ctx, tx, params.LoginRequestID)
			if err != nil {
				return err
			}
			if currentRequest.SessionID != current.ID {
				return fmt.Errorf("browser session login request %d does not belong to session %d", currentRequest.ID, current.ID)
			}
			if currentRequest.Status != BrowserSessionLoginRequestStatusRequested {
				return fmt.Errorf("browser session login request status %q cannot complete", currentRequest.Status)
			}
			request = &currentRequest
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE browser_session_profiles
			SET status = ?, updated_at = ?, last_verified_at = ?
			WHERE id = ?
		`, string(BrowserSessionStatusVerified), formatTime(now), formatTime(now), current.ID); err != nil {
			return err
		}
		verified = current
		verified.Status = BrowserSessionStatusVerified
		verified.UpdatedAt = now
		verified.LastVerifiedAt = &now
		actor := defaultString(params.Actor, "operator")
		reason := strings.TrimSpace(params.Reason)
		if err := appendBrowserSessionEventTx(ctx, tx, verified, runtimeevents.EventBrowserSessionStatusChanged, runtimeevents.BrowserSessionStatusChangedPayload{
			SessionID:      verified.ID,
			PreviousStatus: string(current.Status),
			Status:         string(verified.Status),
			Actor:          actor,
			Reason:         reason,
			LastVerifiedAt: formatTime(now),
			ExpiresAt:      formatOptionalTime(verified.ExpiresAt),
		}, now); err != nil {
			return err
		}
		if err := appendBrowserSessionEventTx(ctx, tx, verified, runtimeevents.EventBrowserSessionVerified, runtimeevents.BrowserSessionVerifiedPayload{
			SessionID:      verified.ID,
			PreviousStatus: string(current.Status),
			Status:         string(verified.Status),
			Actor:          actor,
			Reason:         reason,
			LastVerifiedAt: formatTime(now),
			LoginRequestID: params.LoginRequestID,
		}, now); err != nil {
			return err
		}
		if request == nil {
			return nil
		}
		updatedRequest, err := completeBrowserSessionLoginRequestTx(ctx, tx, *request, verified, now)
		if err != nil {
			return err
		}
		completed = &updatedRequest
		return nil
	})
	return verified, completed, err
}

func (store *Store) RecordBrowserSessionProfilePrepared(ctx context.Context, params RecordBrowserSessionProfilePreparedParams) error {
	if params.SessionID <= 0 {
		return fmt.Errorf("browser session id must be positive")
	}
	profilePath, err := ValidateBrowserSessionProfilePath(params.ProfilePath)
	if err != nil {
		return err
	}
	now := store.now()
	return store.withTx(ctx, func(tx *sql.Tx) error {
		session, err := getBrowserSessionTx(ctx, tx, params.SessionID)
		if err != nil {
			return err
		}
		if session.Status == BrowserSessionStatusRevoked {
			return fmt.Errorf("revoked browser session cannot prepare profile")
		}
		if session.ProfilePath != profilePath {
			return fmt.Errorf("browser session profile path mismatch")
		}
		return appendBrowserSessionEventTx(ctx, tx, session, runtimeevents.EventBrowserSessionProfilePrepared, runtimeevents.BrowserSessionProfilePreparedPayload{
			SessionID:            session.ID,
			Status:               string(session.Status),
			ProfileStoragePolicy: string(session.ProfileStoragePolicy),
			ProfilePath:          profilePath,
			Created:              params.Created,
			Actor:                defaultString(params.Actor, "operator"),
		}, now)
	})
}

func (store *Store) CreateBrowserSessionLoginRequest(ctx context.Context, params CreateBrowserSessionLoginRequestParams) (BrowserSessionLoginRequest, error) {
	if params.SessionID <= 0 {
		return BrowserSessionLoginRequest{}, fmt.Errorf("browser session id must be positive")
	}
	expiresAt := params.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		return BrowserSessionLoginRequest{}, fmt.Errorf("browser session login request expires_at is required")
	}
	handoffID, err := store.newBrowserSessionHandoffID()
	if err != nil {
		return BrowserSessionLoginRequest{}, err
	}
	handoffURL, err := buildBrowserSessionHandoffURL(params.HandoffBaseURL, handoffID)
	if err != nil {
		return BrowserSessionLoginRequest{}, err
	}
	now := store.now()
	request := BrowserSessionLoginRequest{
		SessionID:  params.SessionID,
		Status:     BrowserSessionLoginRequestStatusRequested,
		HandoffID:  handoffID,
		HandoffURL: handoffURL,
		ExpiresAt:  expiresAt,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	err = store.withTx(ctx, func(tx *sql.Tx) error {
		session, err := getBrowserSessionTx(ctx, tx, params.SessionID)
		if err != nil {
			return err
		}
		if session.Status == BrowserSessionStatusRevoked {
			return fmt.Errorf("revoked browser session cannot create login request")
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO browser_session_login_requests (
				session_id, status, handoff_id, handoff_url, expires_at, completed_at, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, NULL, ?, ?)
		`,
			request.SessionID,
			string(request.Status),
			request.HandoffID,
			nullStringPtr(request.HandoffURL),
			formatTime(request.ExpiresAt),
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}
		request.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		return appendBrowserSessionEventTx(ctx, tx, session, runtimeevents.EventBrowserSessionLoginRequested, runtimeevents.BrowserSessionLoginRequestedPayload{
			SessionID:      session.ID,
			LoginRequestID: request.ID,
			Status:         string(request.Status),
			HandoffID:      request.HandoffID,
			HandoffURL:     stringPtrValue(request.HandoffURL),
			ExpiresAt:      formatTime(request.ExpiresAt),
		}, now)
	})
	return request, err
}

func (store *Store) GetBrowserSessionLoginRequest(ctx context.Context, id int64) (BrowserSessionLoginRequest, error) {
	if id <= 0 {
		return BrowserSessionLoginRequest{}, fmt.Errorf("browser session login request id must be positive")
	}
	row := store.db.QueryRowContext(ctx, browserSessionLoginRequestSelectSQL()+` WHERE id = ?`, id)
	return scanBrowserSessionLoginRequest(row)
}

func (store *Store) GetBrowserSessionLoginHandoff(ctx context.Context, handoffID string) (BrowserSessionLoginHandoff, error) {
	handoffID = strings.TrimSpace(handoffID)
	if handoffID == "" {
		return BrowserSessionLoginHandoff{}, fmt.Errorf("browser session handoff id is required")
	}
	row := store.db.QueryRowContext(ctx, browserSessionLoginRequestSelectSQL()+` WHERE handoff_id = ?`, handoffID)
	request, err := scanBrowserSessionLoginRequest(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BrowserSessionLoginHandoff{}, fmt.Errorf("browser session handoff %q not found", handoffID)
		}
		return BrowserSessionLoginHandoff{}, err
	}
	if request.Status != BrowserSessionLoginRequestStatusRequested {
		return BrowserSessionLoginHandoff{}, fmt.Errorf("browser session login request status %q cannot use handoff", request.Status)
	}
	if !request.ExpiresAt.After(store.now()) {
		return BrowserSessionLoginHandoff{}, fmt.Errorf("browser session handoff %q expired at %s", handoffID, formatTime(request.ExpiresAt))
	}
	session, err := store.GetBrowserSession(ctx, request.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BrowserSessionLoginHandoff{}, fmt.Errorf("browser session handoff %q linked session %d not found", handoffID, request.SessionID)
		}
		return BrowserSessionLoginHandoff{}, err
	}
	if session.Status == BrowserSessionStatusRevoked {
		return BrowserSessionLoginHandoff{}, fmt.Errorf("revoked browser session cannot use handoff")
	}
	return BrowserSessionLoginHandoff{
		HandoffID:    handoffID,
		LoginRequest: request,
		Session:      session,
	}, nil
}

func (store *Store) ListBrowserSessionLoginRequests(ctx context.Context, params ListBrowserSessionLoginRequestsParams) ([]BrowserSessionLoginRequest, error) {
	if params.SessionID <= 0 {
		return nil, fmt.Errorf("browser session id must be positive")
	}
	rows, err := store.db.QueryContext(ctx, browserSessionLoginRequestSelectSQL()+` WHERE session_id = ? ORDER BY id ASC`, params.SessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests := make([]BrowserSessionLoginRequest, 0)
	for rows.Next() {
		request, err := scanBrowserSessionLoginRequest(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

func (store *Store) CompleteBrowserSessionLoginRequest(ctx context.Context, params CompleteBrowserSessionLoginRequestParams) (BrowserSessionLoginRequest, error) {
	if params.RequestID <= 0 {
		return BrowserSessionLoginRequest{}, fmt.Errorf("browser session login request id must be positive")
	}
	now := store.now()
	var updated BrowserSessionLoginRequest
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := getBrowserSessionLoginRequestTx(ctx, tx, params.RequestID)
		if err != nil {
			return err
		}
		if current.Status == BrowserSessionLoginRequestStatusCompleted {
			updated = current
			return nil
		}
		if current.Status != BrowserSessionLoginRequestStatusRequested {
			return fmt.Errorf("browser session login request status %q cannot complete", current.Status)
		}
		session, err := getBrowserSessionTx(ctx, tx, current.SessionID)
		if err != nil {
			return err
		}
		updated, err = completeBrowserSessionLoginRequestTx(ctx, tx, current, session, now)
		return err
	})
	return updated, err
}

func completeBrowserSessionLoginRequestTx(ctx context.Context, tx *sql.Tx, current BrowserSessionLoginRequest, session BrowserSession, now time.Time) (BrowserSessionLoginRequest, error) {
	if current.Status != BrowserSessionLoginRequestStatusRequested {
		return BrowserSessionLoginRequest{}, fmt.Errorf("browser session login request status %q cannot complete", current.Status)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE browser_session_login_requests
		SET status = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, string(BrowserSessionLoginRequestStatusCompleted), formatTime(now), formatTime(now), current.ID); err != nil {
		return BrowserSessionLoginRequest{}, err
	}
	updated := current
	updated.Status = BrowserSessionLoginRequestStatusCompleted
	updated.CompletedAt = &now
	updated.UpdatedAt = now
	if err := appendBrowserSessionEventTx(ctx, tx, session, runtimeevents.EventBrowserSessionLoginCompleted, runtimeevents.BrowserSessionLoginCompletedPayload{
		SessionID:      session.ID,
		LoginRequestID: updated.ID,
		PreviousStatus: string(current.Status),
		Status:         string(updated.Status),
		CompletedAt:    formatTime(now),
	}, now); err != nil {
		return BrowserSessionLoginRequest{}, err
	}
	return updated, nil
}

func (store *Store) ExpireBrowserSessionLoginRequest(ctx context.Context, params ExpireBrowserSessionLoginRequestParams) (BrowserSessionLoginRequest, error) {
	if params.RequestID <= 0 {
		return BrowserSessionLoginRequest{}, fmt.Errorf("browser session login request id must be positive")
	}
	now := store.now()
	var updated BrowserSessionLoginRequest
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := getBrowserSessionLoginRequestTx(ctx, tx, params.RequestID)
		if err != nil {
			return err
		}
		if current.Status == BrowserSessionLoginRequestStatusExpired {
			updated = current
			return nil
		}
		if current.Status != BrowserSessionLoginRequestStatusRequested {
			return fmt.Errorf("browser session login request status %q cannot expire", current.Status)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE browser_session_login_requests
			SET status = ?, updated_at = ?
			WHERE id = ?
		`, string(BrowserSessionLoginRequestStatusExpired), formatTime(now), current.ID); err != nil {
			return err
		}
		updated = current
		updated.Status = BrowserSessionLoginRequestStatusExpired
		updated.UpdatedAt = now
		session, err := getBrowserSessionTx(ctx, tx, current.SessionID)
		if err != nil {
			return err
		}
		return appendBrowserSessionEventTx(ctx, tx, session, runtimeevents.EventBrowserSessionLoginExpired, runtimeevents.BrowserSessionLoginExpiredPayload{
			SessionID:      session.ID,
			LoginRequestID: updated.ID,
			PreviousStatus: string(current.Status),
			Status:         string(updated.Status),
			ExpiresAt:      formatTime(updated.ExpiresAt),
		}, now)
	})
	return updated, err
}

func (store *Store) CreateBrowserHandoffRunner(ctx context.Context, params CreateBrowserHandoffRunnerParams) (BrowserHandoffRunner, error) {
	if params.SessionID <= 0 {
		return BrowserHandoffRunner{}, fmt.Errorf("browser session id must be positive")
	}
	if params.LoginRequestID <= 0 {
		return BrowserHandoffRunner{}, fmt.Errorf("browser session login request id must be positive")
	}
	handoffID := strings.TrimSpace(params.HandoffID)
	if handoffID == "" {
		return BrowserHandoffRunner{}, fmt.Errorf("browser session handoff id is required")
	}
	expiresAt := params.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		return BrowserHandoffRunner{}, fmt.Errorf("browser handoff runner expires_at is required")
	}
	now := store.now()
	runner := BrowserHandoffRunner{
		SessionID:      params.SessionID,
		LoginRequestID: params.LoginRequestID,
		HandoffID:      handoffID,
		Status:         BrowserHandoffRunnerStatusRequested,
		ViewerURL:      cloneStringPtr(params.ViewerURL),
		RunnerID:       cloneStringPtr(params.RunnerID),
		ProcessID:      cloneInt64Ptr(params.ProcessID),
		BindAddr:       cloneStringPtr(params.BindAddr),
		PrivateBaseURL: cloneStringPtr(params.PrivateBaseURL),
		PublicBaseURL:  cloneStringPtr(params.PublicBaseURL),
		ExpiresAt:      expiresAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		session, err := getBrowserSessionTx(ctx, tx, params.SessionID)
		if err != nil {
			return err
		}
		if session.Status == BrowserSessionStatusRevoked {
			return fmt.Errorf("revoked browser session cannot create handoff runner")
		}
		request, err := getBrowserSessionLoginRequestTx(ctx, tx, params.LoginRequestID)
		if err != nil {
			return err
		}
		if request.SessionID != session.ID {
			return fmt.Errorf("browser session login request %d does not belong to session %d", request.ID, session.ID)
		}
		if request.HandoffID != runner.HandoffID {
			return fmt.Errorf("browser session login request handoff id mismatch")
		}
		if request.Status != BrowserSessionLoginRequestStatusRequested {
			return fmt.Errorf("browser session login request status %q cannot create handoff runner", request.Status)
		}
		if !request.ExpiresAt.After(now) {
			return fmt.Errorf("browser session login request expired at %s", formatTime(request.ExpiresAt))
		}
		if runner.ExpiresAt.After(request.ExpiresAt) {
			return fmt.Errorf("browser handoff runner expires_at cannot exceed login request expires_at")
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO browser_handoff_runners (
				session_id, login_request_id, handoff_id, status, viewer_url, runner_id, process_id,
				bind_addr, private_base_url, public_base_url, expires_at, started_at, completed_at,
				cancelled_at, created_at, updated_at, error_code, error_message
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, ?, ?, NULL, NULL)
		`,
			runner.SessionID,
			runner.LoginRequestID,
			runner.HandoffID,
			string(runner.Status),
			nullStringPtr(runner.ViewerURL),
			nullStringPtr(runner.RunnerID),
			nullInt64Ptr(runner.ProcessID),
			nullStringPtr(runner.BindAddr),
			nullStringPtr(runner.PrivateBaseURL),
			nullStringPtr(runner.PublicBaseURL),
			formatTime(runner.ExpiresAt),
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}
		runner.ID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		return appendBrowserSessionEventTx(ctx, tx, session, runtimeevents.EventBrowserHandoffRunnerRequested, browserHandoffRunnerLifecyclePayload(runner, "", "", ""), now)
	})
	return runner, err
}

func (store *Store) GetBrowserHandoffRunner(ctx context.Context, id int64) (BrowserHandoffRunner, error) {
	if id <= 0 {
		return BrowserHandoffRunner{}, fmt.Errorf("browser handoff runner id must be positive")
	}
	row := store.db.QueryRowContext(ctx, browserHandoffRunnerSelectSQL()+` WHERE id = ?`, id)
	return scanBrowserHandoffRunner(row)
}

func (store *Store) ListBrowserHandoffRunners(ctx context.Context, params ListBrowserHandoffRunnersParams) ([]BrowserHandoffRunner, error) {
	if params.LoginRequestID <= 0 {
		return nil, fmt.Errorf("browser session login request id must be positive")
	}
	rows, err := store.db.QueryContext(ctx, browserHandoffRunnerSelectSQL()+` WHERE login_request_id = ? ORDER BY id ASC`, params.LoginRequestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runners := make([]BrowserHandoffRunner, 0)
	for rows.Next() {
		runner, err := scanBrowserHandoffRunner(rows)
		if err != nil {
			return nil, err
		}
		runners = append(runners, runner)
	}
	return runners, rows.Err()
}

func (store *Store) UpdateBrowserHandoffRunnerStatus(ctx context.Context, params UpdateBrowserHandoffRunnerStatusParams) (BrowserHandoffRunner, error) {
	if params.ID <= 0 {
		return BrowserHandoffRunner{}, fmt.Errorf("browser handoff runner id must be positive")
	}
	status := normalizeBrowserHandoffRunnerStatus(params.Status)
	if status == "" || status == BrowserHandoffRunnerStatusRequested {
		return BrowserHandoffRunner{}, fmt.Errorf("browser handoff runner status is required")
	}
	now := store.now()
	var updated BrowserHandoffRunner
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := getBrowserHandoffRunnerTx(ctx, tx, params.ID)
		if err != nil {
			return err
		}
		if current.Status == status {
			updated = current
			return nil
		}
		if !canTransitionBrowserHandoffRunner(current.Status, status) {
			return fmt.Errorf("browser handoff runner status %q cannot transition to %q", current.Status, status)
		}
		updated = current
		updated.Status = status
		updated.UpdatedAt = now
		if params.ViewerURL != nil {
			updated.ViewerURL = cloneStringPtr(params.ViewerURL)
		}
		if params.RunnerID != nil {
			updated.RunnerID = cloneStringPtr(params.RunnerID)
		}
		if params.ProcessID != nil {
			updated.ProcessID = cloneInt64Ptr(params.ProcessID)
		}
		if params.BindAddr != nil {
			updated.BindAddr = cloneStringPtr(params.BindAddr)
		}
		if params.PrivateBaseURL != nil {
			updated.PrivateBaseURL = cloneStringPtr(params.PrivateBaseURL)
		}
		if params.PublicBaseURL != nil {
			updated.PublicBaseURL = cloneStringPtr(params.PublicBaseURL)
		}
		if params.ErrorCode != nil {
			updated.ErrorCode = cloneStringPtr(params.ErrorCode)
		}
		if params.ErrorMessage != nil {
			updated.ErrorMessage = cloneStringPtr(params.ErrorMessage)
		}
		switch status {
		case BrowserHandoffRunnerStatusStarted:
			if updated.StartedAt == nil {
				updated.StartedAt = &now
			}
		case BrowserHandoffRunnerStatusCompleted:
			updated.CompletedAt = &now
			updated.ExitedAt = &now
		case BrowserHandoffRunnerStatusExpired,
			BrowserHandoffRunnerStatusFailed:
			updated.ExitedAt = &now
		case BrowserHandoffRunnerStatusCancelled:
			updated.CancelledAt = &now
			updated.ExitedAt = &now
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE browser_handoff_runners
			SET status = ?, viewer_url = ?, runner_id = ?, process_id = ?, bind_addr = ?,
				private_base_url = ?, public_base_url = ?, started_at = ?, exited_at = ?, completed_at = ?,
				cancelled_at = ?, updated_at = ?, error_code = ?, error_message = ?
			WHERE id = ?
		`,
			string(updated.Status),
			nullStringPtr(updated.ViewerURL),
			nullStringPtr(updated.RunnerID),
			nullInt64Ptr(updated.ProcessID),
			nullStringPtr(updated.BindAddr),
			nullStringPtr(updated.PrivateBaseURL),
			nullStringPtr(updated.PublicBaseURL),
			nullTime(updated.StartedAt),
			nullTime(updated.ExitedAt),
			nullTime(updated.CompletedAt),
			nullTime(updated.CancelledAt),
			formatTime(now),
			nullStringPtr(updated.ErrorCode),
			nullStringPtr(updated.ErrorMessage),
			updated.ID,
		); err != nil {
			return err
		}
		session, err := getBrowserSessionTx(ctx, tx, updated.SessionID)
		if err != nil {
			return err
		}
		eventType, ok := browserHandoffRunnerEventType(status)
		if !ok {
			return nil
		}
		return appendBrowserSessionEventTx(ctx, tx, session, eventType, browserHandoffRunnerLifecyclePayload(updated, current.Status, params.Actor, params.Reason), now)
	})
	return updated, err
}

func (store *Store) ExpireBrowserHandoffRunner(ctx context.Context, params ExpireBrowserHandoffRunnerParams) (BrowserHandoffRunner, error) {
	return store.UpdateBrowserHandoffRunnerStatus(ctx, UpdateBrowserHandoffRunnerStatusParams{
		ID:     params.ID,
		Status: BrowserHandoffRunnerStatusExpired,
		Actor:  params.Actor,
		Reason: params.Reason,
	})
}

func (store *Store) CancelBrowserHandoffRunner(ctx context.Context, params CancelBrowserHandoffRunnerParams) (BrowserHandoffRunner, error) {
	return store.UpdateBrowserHandoffRunnerStatus(ctx, UpdateBrowserHandoffRunnerStatusParams{
		ID:     params.ID,
		Status: BrowserHandoffRunnerStatusCancelled,
		Actor:  params.Actor,
		Reason: params.Reason,
	})
}

func appendBrowserSessionEventTx(ctx context.Context, tx *sql.Tx, session BrowserSession, eventType runtimeevents.Type, payload any, occurredAt time.Time) error {
	return appendEventTx(ctx, tx, eventInsert{
		StreamType: runtimeevents.StreamBrowserSession,
		StreamID:   session.ID,
		EventType:  eventType,
		Scope:      "browser_session",
		Payload:    payload,
		OccurredAt: occurredAt,
	})
}

func getBrowserSessionTx(ctx context.Context, tx *sql.Tx, id int64) (BrowserSession, error) {
	row := tx.QueryRowContext(ctx, browserSessionSelectSQL()+` WHERE id = ?`, id)
	return scanBrowserSession(row)
}

func getBrowserSessionLoginRequestTx(ctx context.Context, tx *sql.Tx, id int64) (BrowserSessionLoginRequest, error) {
	row := tx.QueryRowContext(ctx, browserSessionLoginRequestSelectSQL()+` WHERE id = ?`, id)
	return scanBrowserSessionLoginRequest(row)
}

func getBrowserHandoffRunnerTx(ctx context.Context, tx *sql.Tx, id int64) (BrowserHandoffRunner, error) {
	row := tx.QueryRowContext(ctx, browserHandoffRunnerSelectSQL()+` WHERE id = ?`, id)
	return scanBrowserHandoffRunner(row)
}

func browserSessionSelectSQL() string {
	return `
		SELECT id, name, domain, account_hint, permission_tier, status, profile_storage_policy, profile_path,
			created_at, updated_at, last_verified_at, expires_at, revoked_at
		FROM browser_session_profiles
	`
}

func browserSessionLoginRequestSelectSQL() string {
	return `
		SELECT id, session_id, status, handoff_id, handoff_url, expires_at, completed_at, created_at, updated_at
		FROM browser_session_login_requests
	`
}

func browserHandoffRunnerSelectSQL() string {
	return `
		SELECT id, session_id, login_request_id, handoff_id, status, viewer_url, runner_id, process_id,
			bind_addr, private_base_url, public_base_url, expires_at, started_at, exited_at, completed_at, cancelled_at,
			created_at, updated_at, error_code, error_message
		FROM browser_handoff_runners
	`
}

type browserSessionScanner interface {
	Scan(dest ...any) error
}

func scanBrowserSession(scanner browserSessionScanner) (BrowserSession, error) {
	var session BrowserSession
	var createdAt, updatedAt string
	var lastVerifiedAt, expiresAt, revokedAt sql.NullString
	if err := scanner.Scan(
		&session.ID,
		&session.Name,
		&session.Domain,
		&session.AccountHint,
		&session.PermissionTier,
		&session.Status,
		&session.ProfileStoragePolicy,
		&session.ProfilePath,
		&createdAt,
		&updatedAt,
		&lastVerifiedAt,
		&expiresAt,
		&revokedAt,
	); err != nil {
		return BrowserSession{}, err
	}
	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return BrowserSession{}, err
	}
	session.CreatedAt = parsedCreatedAt
	parsedUpdatedAt, err := parseTime(updatedAt)
	if err != nil {
		return BrowserSession{}, err
	}
	session.UpdatedAt = parsedUpdatedAt
	session.LastVerifiedAt, err = parseNullableTime(lastVerifiedAt)
	if err != nil {
		return BrowserSession{}, err
	}
	session.ExpiresAt, err = parseNullableTime(expiresAt)
	if err != nil {
		return BrowserSession{}, err
	}
	session.RevokedAt, err = parseNullableTime(revokedAt)
	if err != nil {
		return BrowserSession{}, err
	}
	session.ProfileStoragePolicy = normalizeBrowserSessionProfileStoragePolicy(session.ProfileStoragePolicy)
	if session.ProfileStoragePolicy == "" {
		session.ProfileStoragePolicy = BrowserSessionProfileStoragePolicyEncryptedRequired
	}
	return session, nil
}

func scanBrowserSessionLoginRequest(scanner browserSessionScanner) (BrowserSessionLoginRequest, error) {
	var request BrowserSessionLoginRequest
	var handoffURL, completedAt sql.NullString
	var expiresAt, createdAt, updatedAt string
	if err := scanner.Scan(
		&request.ID,
		&request.SessionID,
		&request.Status,
		&request.HandoffID,
		&handoffURL,
		&expiresAt,
		&completedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return BrowserSessionLoginRequest{}, err
	}
	request.HandoffURL = nullableStringPtr(handoffURL)
	parsedExpiresAt, err := parseTime(expiresAt)
	if err != nil {
		return BrowserSessionLoginRequest{}, err
	}
	request.ExpiresAt = parsedExpiresAt
	request.CompletedAt, err = parseNullableTime(completedAt)
	if err != nil {
		return BrowserSessionLoginRequest{}, err
	}
	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return BrowserSessionLoginRequest{}, err
	}
	request.CreatedAt = parsedCreatedAt
	parsedUpdatedAt, err := parseTime(updatedAt)
	if err != nil {
		return BrowserSessionLoginRequest{}, err
	}
	request.UpdatedAt = parsedUpdatedAt
	return request, nil
}

func scanBrowserHandoffRunner(scanner browserSessionScanner) (BrowserHandoffRunner, error) {
	var runner BrowserHandoffRunner
	var viewerURL, runnerID, bindAddr, privateBaseURL, publicBaseURL sql.NullString
	var processID sql.NullInt64
	var expiresAt, createdAt, updatedAt string
	var startedAt, exitedAt, completedAt, cancelledAt, errorCode, errorMessage sql.NullString
	if err := scanner.Scan(
		&runner.ID,
		&runner.SessionID,
		&runner.LoginRequestID,
		&runner.HandoffID,
		&runner.Status,
		&viewerURL,
		&runnerID,
		&processID,
		&bindAddr,
		&privateBaseURL,
		&publicBaseURL,
		&expiresAt,
		&startedAt,
		&exitedAt,
		&completedAt,
		&cancelledAt,
		&createdAt,
		&updatedAt,
		&errorCode,
		&errorMessage,
	); err != nil {
		return BrowserHandoffRunner{}, err
	}
	runner.Status = normalizeBrowserHandoffRunnerStatus(runner.Status)
	if runner.Status == "" {
		return BrowserHandoffRunner{}, fmt.Errorf("unsupported browser handoff runner status")
	}
	runner.ViewerURL = nullableStringPtr(viewerURL)
	runner.RunnerID = nullableStringPtr(runnerID)
	runner.ProcessID = nullableInt64Ptr(processID)
	runner.BindAddr = nullableStringPtr(bindAddr)
	runner.PrivateBaseURL = nullableStringPtr(privateBaseURL)
	runner.PublicBaseURL = nullableStringPtr(publicBaseURL)
	parsedExpiresAt, err := parseTime(expiresAt)
	if err != nil {
		return BrowserHandoffRunner{}, err
	}
	runner.ExpiresAt = parsedExpiresAt
	runner.StartedAt, err = parseNullableTime(startedAt)
	if err != nil {
		return BrowserHandoffRunner{}, err
	}
	runner.ExitedAt, err = parseNullableTime(exitedAt)
	if err != nil {
		return BrowserHandoffRunner{}, err
	}
	runner.CompletedAt, err = parseNullableTime(completedAt)
	if err != nil {
		return BrowserHandoffRunner{}, err
	}
	runner.CancelledAt, err = parseNullableTime(cancelledAt)
	if err != nil {
		return BrowserHandoffRunner{}, err
	}
	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return BrowserHandoffRunner{}, err
	}
	runner.CreatedAt = parsedCreatedAt
	parsedUpdatedAt, err := parseTime(updatedAt)
	if err != nil {
		return BrowserHandoffRunner{}, err
	}
	runner.UpdatedAt = parsedUpdatedAt
	runner.ErrorCode = nullableStringPtr(errorCode)
	runner.ErrorMessage = nullableStringPtr(errorMessage)
	return runner, nil
}

func (store *Store) newBrowserSessionHandoffID() (string, error) {
	if store.BrowserSessionHandoffID != nil {
		id, err := store.BrowserSessionHandoffID()
		if err != nil {
			return "", err
		}
		id = strings.TrimSpace(id)
		if id == "" {
			return "", fmt.Errorf("browser session handoff id is required")
		}
		return id, nil
	}
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("browser session handoff id: %w", err)
	}
	return hex.EncodeToString(data[:]), nil
}

func buildBrowserSessionHandoffURL(baseURL string, handoffID string) (*string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed == nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("handoff base URL must be an absolute http or https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("handoff base URL must use http or https")
	}
	query := parsed.Query()
	query.Set("handoff_id", handoffID)
	parsed.RawQuery = query.Encode()
	result := parsed.String()
	return &result, nil
}

func normalizeBrowserSessionStatus(status BrowserSessionStatus) BrowserSessionStatus {
	switch BrowserSessionStatus(strings.ToLower(strings.TrimSpace(string(status)))) {
	case BrowserSessionStatusCreated:
		return BrowserSessionStatusCreated
	case BrowserSessionStatusLoginRequested:
		return BrowserSessionStatusLoginRequested
	case BrowserSessionStatusVerified:
		return BrowserSessionStatusVerified
	case BrowserSessionStatusExpired:
		return BrowserSessionStatusExpired
	case BrowserSessionStatusRevoked:
		return BrowserSessionStatusRevoked
	default:
		return ""
	}
}

func normalizeBrowserSessionPermissionTier(tier BrowserSessionPermissionTier) BrowserSessionPermissionTier {
	switch BrowserSessionPermissionTier(strings.ToLower(strings.TrimSpace(string(tier)))) {
	case BrowserSessionPermissionTierPublicReadOnly:
		return BrowserSessionPermissionTierPublicReadOnly
	case BrowserSessionPermissionTierAuthenticatedReadOnly:
		return BrowserSessionPermissionTierAuthenticatedReadOnly
	default:
		return ""
	}
}

func normalizeBrowserSessionProfileStoragePolicy(policy BrowserSessionProfileStoragePolicy) BrowserSessionProfileStoragePolicy {
	switch BrowserSessionProfileStoragePolicy(strings.ToLower(strings.TrimSpace(string(policy)))) {
	case BrowserSessionProfileStoragePolicyDisabled:
		return BrowserSessionProfileStoragePolicyDisabled
	case BrowserSessionProfileStoragePolicyPreparedUnencrypted:
		return BrowserSessionProfileStoragePolicyPreparedUnencrypted
	case BrowserSessionProfileStoragePolicyEncryptedRequired:
		return BrowserSessionProfileStoragePolicyEncryptedRequired
	default:
		return ""
	}
}

func CanWriteBrowserProfile(session BrowserSession) bool {
	if session.Status == BrowserSessionStatusRevoked {
		return false
	}
	switch normalizeBrowserSessionProfileStoragePolicy(session.ProfileStoragePolicy) {
	case BrowserSessionProfileStoragePolicyDisabled,
		BrowserSessionProfileStoragePolicyPreparedUnencrypted,
		BrowserSessionProfileStoragePolicyEncryptedRequired:
		return false
	default:
		return false
	}
}

func normalizeBrowserSessionLoginRequestStatus(status BrowserSessionLoginRequestStatus) BrowserSessionLoginRequestStatus {
	switch BrowserSessionLoginRequestStatus(strings.ToLower(strings.TrimSpace(string(status)))) {
	case BrowserSessionLoginRequestStatusRequested:
		return BrowserSessionLoginRequestStatusRequested
	case BrowserSessionLoginRequestStatusCompleted:
		return BrowserSessionLoginRequestStatusCompleted
	case BrowserSessionLoginRequestStatusExpired:
		return BrowserSessionLoginRequestStatusExpired
	case BrowserSessionLoginRequestStatusCancelled:
		return BrowserSessionLoginRequestStatusCancelled
	default:
		return ""
	}
}

func normalizeBrowserHandoffRunnerStatus(status BrowserHandoffRunnerStatus) BrowserHandoffRunnerStatus {
	switch BrowserHandoffRunnerStatus(strings.ToLower(strings.TrimSpace(string(status)))) {
	case BrowserHandoffRunnerStatusRequested:
		return BrowserHandoffRunnerStatusRequested
	case BrowserHandoffRunnerStatusStarted:
		return BrowserHandoffRunnerStatusStarted
	case BrowserHandoffRunnerStatusCompleted:
		return BrowserHandoffRunnerStatusCompleted
	case BrowserHandoffRunnerStatusExpired:
		return BrowserHandoffRunnerStatusExpired
	case BrowserHandoffRunnerStatusCancelled:
		return BrowserHandoffRunnerStatusCancelled
	case BrowserHandoffRunnerStatusFailed:
		return BrowserHandoffRunnerStatusFailed
	default:
		return ""
	}
}

func canTransitionBrowserHandoffRunner(current BrowserHandoffRunnerStatus, next BrowserHandoffRunnerStatus) bool {
	switch current {
	case BrowserHandoffRunnerStatusRequested:
		return next == BrowserHandoffRunnerStatusStarted ||
			next == BrowserHandoffRunnerStatusExpired ||
			next == BrowserHandoffRunnerStatusCancelled ||
			next == BrowserHandoffRunnerStatusFailed
	case BrowserHandoffRunnerStatusStarted:
		return next == BrowserHandoffRunnerStatusCompleted ||
			next == BrowserHandoffRunnerStatusExpired ||
			next == BrowserHandoffRunnerStatusCancelled ||
			next == BrowserHandoffRunnerStatusFailed
	case BrowserHandoffRunnerStatusCompleted,
		BrowserHandoffRunnerStatusExpired,
		BrowserHandoffRunnerStatusCancelled,
		BrowserHandoffRunnerStatusFailed:
		return false
	default:
		return false
	}
}

func browserHandoffRunnerEventType(status BrowserHandoffRunnerStatus) (runtimeevents.Type, bool) {
	switch status {
	case BrowserHandoffRunnerStatusStarted:
		return runtimeevents.EventBrowserHandoffRunnerStarted, true
	case BrowserHandoffRunnerStatusCompleted:
		return runtimeevents.EventBrowserHandoffRunnerCompleted, true
	case BrowserHandoffRunnerStatusExpired:
		return runtimeevents.EventBrowserHandoffRunnerExpired, true
	case BrowserHandoffRunnerStatusCancelled:
		return runtimeevents.EventBrowserHandoffRunnerCancelled, true
	case BrowserHandoffRunnerStatusFailed:
		return runtimeevents.EventBrowserHandoffRunnerFailed, true
	default:
		return "", false
	}
}

func browserHandoffRunnerLifecyclePayload(runner BrowserHandoffRunner, previousStatus BrowserHandoffRunnerStatus, actor string, reason string) runtimeevents.BrowserHandoffRunnerLifecyclePayload {
	return runtimeevents.BrowserHandoffRunnerLifecyclePayload{
		ID:             runner.ID,
		SessionID:      runner.SessionID,
		LoginRequestID: runner.LoginRequestID,
		HandoffID:      runner.HandoffID,
		RunnerID:       stringPtrValue(runner.RunnerID),
		ProcessID:      int64PtrValue(runner.ProcessID),
		PreviousStatus: string(previousStatus),
		Status:         string(runner.Status),
		ViewerURL:      stringPtrValue(runner.ViewerURL),
		BindAddr:       stringPtrValue(runner.BindAddr),
		PrivateBaseURL: stringPtrValue(runner.PrivateBaseURL),
		PublicBaseURL:  stringPtrValue(runner.PublicBaseURL),
		ExpiresAt:      formatTime(runner.ExpiresAt),
		StartedAt:      formatOptionalTime(runner.StartedAt),
		ExitedAt:       formatOptionalTime(runner.ExitedAt),
		CompletedAt:    formatOptionalTime(runner.CompletedAt),
		CancelledAt:    formatOptionalTime(runner.CancelledAt),
		ErrorCode:      stringPtrValue(runner.ErrorCode),
		ErrorMessage:   stringPtrValue(runner.ErrorMessage),
		Actor:          defaultString(actor, "operator"),
		Reason:         strings.TrimSpace(reason),
	}
}

func normalizeBrowserSessionDomain(domain string) (string, error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" {
		return "", fmt.Errorf("browser session domain is required")
	}
	if strings.Contains(domain, "/") || strings.Contains(domain, ":") || strings.Contains(domain, "@") {
		return "", fmt.Errorf("browser session domain must be a hostname")
	}
	return domain, nil
}

func normalizeBrowserSessionProfilePath(profilePath string, sessionName string) (string, error) {
	profilePath = strings.TrimSpace(profilePath)
	if profilePath == "" {
		return filepath.ToSlash(filepath.Join("browser-sessions", "profiles", browserSessionPathSegment(sessionName))), nil
	}
	return ValidateBrowserSessionProfilePath(profilePath)
}

func ValidateBrowserSessionProfilePath(profilePath string) (string, error) {
	profilePath = strings.TrimSpace(profilePath)
	if filepath.IsAbs(profilePath) {
		return "", fmt.Errorf("browser session profile path must be relative to ODIN_ROOT")
	}
	if hasPathTraversalSegment(profilePath) {
		return "", fmt.Errorf("browser session profile path must stay under ODIN_ROOT")
	}
	clean := filepath.Clean(profilePath)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("browser session profile path must stay under ODIN_ROOT")
	}
	slashed := filepath.ToSlash(clean)
	const profileRoot = "browser-sessions/profiles/"
	if !strings.HasPrefix(slashed, profileRoot) || strings.TrimSpace(strings.TrimPrefix(slashed, profileRoot)) == "" {
		return "", fmt.Errorf("browser session profile path must stay under browser-sessions/profiles")
	}
	component := strings.TrimPrefix(slashed, profileRoot)
	if strings.Contains(component, "/") {
		return "", fmt.Errorf("browser session profile path must use a single safe component")
	}
	if !isSafeBrowserSessionPathSegment(component) {
		return "", fmt.Errorf("browser session profile path contains unsafe component %q", component)
	}
	return slashed, nil
}

func browserSessionPathSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	segment := strings.Trim(builder.String(), "-._")
	if segment == "" {
		return "session"
	}
	return segment
}

func hasPathTraversalSegment(value string) bool {
	for _, component := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if component == ".." {
			return true
		}
	}
	return false
}

func isSafeBrowserSessionPathSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullStringPtr(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullInt64Ptr(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	ptr := new(string)
	*ptr = value.String
	return ptr
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64PtrValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
