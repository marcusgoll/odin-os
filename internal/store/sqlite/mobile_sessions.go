package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
)

var ErrMobileSessionUnauthorized = errors.New("mobile session unauthorized")

func (store *Store) CreateMobileDeviceSession(ctx context.Context, params CreateMobileDeviceSessionParams) (MobileAuthenticatedSession, error) {
	now := store.now()
	deviceID := strings.TrimSpace(params.DeviceID)
	deviceName := strings.TrimSpace(params.DeviceName)
	if deviceID == "" {
		return MobileAuthenticatedSession{}, fmt.Errorf("device id is required")
	}
	if deviceName == "" {
		deviceName = "Odin mobile device"
	}
	if strings.TrimSpace(params.TokenSHA256) == "" {
		return MobileAuthenticatedSession{}, fmt.Errorf("session token hash is required")
	}
	if strings.TrimSpace(params.CSRFSHA256) == "" {
		return MobileAuthenticatedSession{}, fmt.Errorf("csrf token hash is required")
	}
	if params.ExpiresAt.IsZero() || !params.ExpiresAt.After(now) {
		return MobileAuthenticatedSession{}, fmt.Errorf("session expiry must be in the future")
	}
	actor := strings.TrimSpace(params.Actor)
	if actor == "" {
		actor = "mobile-api"
	}

	var auth MobileAuthenticatedSession
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO mobile_devices (device_id, device_name, status, registered_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, deviceID, deviceName, string(MobileDeviceStatusActive), formatTime(now), formatTime(now), formatTime(now))
		if err != nil {
			return err
		}
		deviceRowID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		sessionResult, err := tx.ExecContext(ctx, `
			INSERT INTO mobile_sessions (device_row_id, token_sha256, csrf_sha256, status, expires_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, deviceRowID, params.TokenSHA256, params.CSRFSHA256, string(MobileSessionStatusActive), formatTime(params.ExpiresAt), formatTime(now), formatTime(now))
		if err != nil {
			return err
		}
		sessionID, err := sessionResult.LastInsertId()
		if err != nil {
			return err
		}
		auth.Device, err = getMobileDeviceByRowIDTx(ctx, tx, deviceRowID)
		if err != nil {
			return err
		}
		auth.Session, err = getMobileSessionByIDTx(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		return appendMobileDeviceEventTx(ctx, tx, auth.Device, runtimeevents.EventMobileLogin, runtimeevents.MobileLoginPayload{
			DeviceID:   auth.Device.DeviceID,
			DeviceName: auth.Device.DeviceName,
			SessionID:  auth.Session.ID,
			Actor:      actor,
		}, now)
	})
	return auth, err
}

func (store *Store) GetMobileSessionByTokenHash(ctx context.Context, params GetMobileSessionByTokenHashParams) (MobileAuthenticatedSession, error) {
	tokenHash := strings.TrimSpace(params.TokenSHA256)
	if tokenHash == "" {
		return MobileAuthenticatedSession{}, ErrMobileSessionUnauthorized
	}
	auth, err := scanMobileAuthenticatedSession(store.db.QueryRowContext(ctx, mobileAuthenticatedSessionSelectSQL()+` WHERE s.token_sha256 = ?`, tokenHash))
	if err != nil {
		return MobileAuthenticatedSession{}, err
	}
	now := store.now()
	if auth.Device.Status != MobileDeviceStatusActive || auth.Session.Status != MobileSessionStatusActive {
		return MobileAuthenticatedSession{}, ErrMobileSessionUnauthorized
	}
	if !auth.Session.ExpiresAt.After(now) {
		_ = store.expireMobileSession(ctx, auth.Session.ID, now)
		return MobileAuthenticatedSession{}, ErrMobileSessionUnauthorized
	}
	if _, err := store.db.ExecContext(ctx, `
		UPDATE mobile_devices SET last_seen_at = ?, updated_at = ? WHERE id = ?
	`, formatTime(now), formatTime(now), auth.Device.ID); err != nil {
		return MobileAuthenticatedSession{}, err
	}
	auth.Device.LastSeenAt = &now
	return auth, nil
}

func (store *Store) RevokeMobileDevice(ctx context.Context, params RevokeMobileDeviceParams) (MobileDevice, error) {
	now := store.now()
	deviceID := strings.TrimSpace(params.DeviceID)
	if deviceID == "" {
		return MobileDevice{}, fmt.Errorf("device id is required")
	}
	actor := strings.TrimSpace(params.Actor)
	if actor == "" {
		actor = "mobile-api"
	}
	var device MobileDevice
	var sessionIDs []int64
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		current, err := getMobileDeviceByDeviceIDTx(ctx, tx, deviceID)
		if err != nil {
			return err
		}
		if current.Status == MobileDeviceStatusRevoked {
			device = current
			return nil
		}
		rows, err := tx.QueryContext(ctx, `SELECT id FROM mobile_sessions WHERE device_row_id = ? AND status = ?`, current.ID, string(MobileSessionStatusActive))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			sessionIDs = append(sessionIDs, id)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE mobile_devices SET status = ?, revoked_at = ?, updated_at = ? WHERE id = ?
		`, string(MobileDeviceStatusRevoked), formatTime(now), formatTime(now), current.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE mobile_sessions SET status = ?, revoked_at = ?, updated_at = ?
			WHERE device_row_id = ? AND status = ?
		`, string(MobileSessionStatusRevoked), formatTime(now), formatTime(now), current.ID, string(MobileSessionStatusActive)); err != nil {
			return err
		}
		device, err = getMobileDeviceByRowIDTx(ctx, tx, current.ID)
		if err != nil {
			return err
		}
		sessionID := int64(0)
		if len(sessionIDs) > 0 {
			sessionID = sessionIDs[0]
		}
		return appendMobileDeviceEventTx(ctx, tx, device, runtimeevents.EventMobileLogout, runtimeevents.MobileLogoutPayload{
			DeviceID:  device.DeviceID,
			SessionID: sessionID,
			Actor:     actor,
			Reason:    params.Reason,
		}, now)
	})
	return device, err
}

func (store *Store) RecordMobileIntakeEvent(ctx context.Context, params RecordMobileIntakeEventParams) error {
	return store.recordMobileEvent(ctx, params.DeviceID, runtimeevents.EventMobileIntakeCreated, runtimeevents.MobileIntakeCreatedPayload{
		DeviceID:     params.DeviceID,
		SessionID:    params.SessionID,
		IntakeItemID: params.IntakeItemID,
		IntakeType:   params.IntakeType,
	})
}

func (store *Store) RecordMobileApprovalEvent(ctx context.Context, params RecordMobileApprovalEventParams) error {
	return store.recordMobileEvent(ctx, params.DeviceID, runtimeevents.EventMobileApprovalResolved, runtimeevents.MobileApprovalResolvedPayload{
		DeviceID:   params.DeviceID,
		SessionID:  params.SessionID,
		ApprovalID: params.ApprovalID,
		Action:     params.Action,
	})
}

func (store *Store) CreateMobilePushSubscription(ctx context.Context, params CreateMobilePushSubscriptionParams) (MobilePushSubscription, error) {
	now := store.now()
	var subscription MobilePushSubscription
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		device, err := getMobileDeviceByDeviceIDTx(ctx, tx, params.DeviceID)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO mobile_push_subscriptions (device_row_id, endpoint_sha256, endpoint_host, user_agent, platform, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(device_row_id, endpoint_sha256) DO UPDATE SET
				endpoint_host = excluded.endpoint_host,
				user_agent = excluded.user_agent,
				platform = excluded.platform,
				status = excluded.status,
				revoked_at = NULL,
				updated_at = excluded.updated_at
		`, device.ID, params.EndpointSHA256, params.EndpointHost, params.UserAgent, params.Platform, string(MobilePushSubscriptionStatusActive), formatTime(now), formatTime(now))
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if id == 0 {
			row := tx.QueryRowContext(ctx, mobilePushSubscriptionSelectSQL()+` WHERE device_row_id = ? AND endpoint_sha256 = ?`, device.ID, params.EndpointSHA256)
			subscription, err = scanMobilePushSubscription(row)
			return err
		}
		subscription, err = scanMobilePushSubscription(tx.QueryRowContext(ctx, mobilePushSubscriptionSelectSQL()+` WHERE id = ?`, id))
		return err
	})
	return subscription, err
}

func (store *Store) RevokeMobilePushSubscription(ctx context.Context, params RevokeMobilePushSubscriptionParams) (MobilePushSubscription, error) {
	now := store.now()
	var subscription MobilePushSubscription
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		device, err := getMobileDeviceByDeviceIDTx(ctx, tx, params.DeviceID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE mobile_push_subscriptions
			SET status = ?, revoked_at = ?, updated_at = ?
			WHERE id = ? AND device_row_id = ?
		`, string(MobilePushSubscriptionStatusRevoked), formatTime(now), formatTime(now), params.SubscriptionID, device.ID); err != nil {
			return err
		}
		subscription, err = scanMobilePushSubscription(tx.QueryRowContext(ctx, mobilePushSubscriptionSelectSQL()+` WHERE id = ? AND device_row_id = ?`, params.SubscriptionID, device.ID))
		if err != nil {
			return err
		}
		return appendMobileDeviceEventTx(ctx, tx, device, runtimeevents.EventMobilePushSubscriptionRevoked, runtimeevents.MobilePushSubscriptionRevokedPayload{
			DeviceID:       device.DeviceID,
			SubscriptionID: subscription.ID,
			Actor:          params.Actor,
			Reason:         params.Reason,
		}, now)
	})
	return subscription, err
}

func (store *Store) expireMobileSession(ctx context.Context, sessionID int64, now time.Time) error {
	_, err := store.db.ExecContext(ctx, `
		UPDATE mobile_sessions SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?
	`, string(MobileSessionStatusExpired), formatTime(now), sessionID, string(MobileSessionStatusActive))
	return err
}

func (store *Store) recordMobileEvent(ctx context.Context, deviceID string, eventType runtimeevents.Type, payload any) error {
	now := store.now()
	return store.withTx(ctx, func(tx *sql.Tx) error {
		device, err := getMobileDeviceByDeviceIDTx(ctx, tx, deviceID)
		if err != nil {
			return err
		}
		return appendMobileDeviceEventTx(ctx, tx, device, eventType, payload, now)
	})
}

func appendMobileDeviceEventTx(ctx context.Context, tx *sql.Tx, device MobileDevice, eventType runtimeevents.Type, payload any, occurredAt time.Time) error {
	return appendEventTx(ctx, tx, eventInsert{
		StreamType: runtimeevents.StreamMobileDevice,
		StreamID:   device.ID,
		EventType:  eventType,
		Scope:      "mobile_device",
		Payload:    payload,
		OccurredAt: occurredAt,
	})
}

func mobileAuthenticatedSessionSelectSQL() string {
	return `
		SELECT
			d.id, d.device_id, d.device_name, d.status, d.registered_at, d.last_seen_at, d.revoked_at, d.created_at, d.updated_at,
			s.id, s.device_row_id, s.token_sha256, s.csrf_sha256, s.status, s.expires_at, s.revoked_at, s.created_at, s.updated_at
		FROM mobile_sessions s
		JOIN mobile_devices d ON d.id = s.device_row_id
	`
}

func mobileDeviceSelectSQL() string {
	return `
		SELECT id, device_id, device_name, status, registered_at, last_seen_at, revoked_at, created_at, updated_at
		FROM mobile_devices
	`
}

func mobileSessionSelectSQL() string {
	return `
		SELECT id, device_row_id, token_sha256, csrf_sha256, status, expires_at, revoked_at, created_at, updated_at
		FROM mobile_sessions
	`
}

func mobilePushSubscriptionSelectSQL() string {
	return `
		SELECT id, device_row_id, endpoint_sha256, endpoint_host, user_agent, platform, status, revoked_at, created_at, updated_at
		FROM mobile_push_subscriptions
	`
}

func getMobileDeviceByRowIDTx(ctx context.Context, tx *sql.Tx, id int64) (MobileDevice, error) {
	return scanMobileDevice(tx.QueryRowContext(ctx, mobileDeviceSelectSQL()+` WHERE id = ?`, id))
}

func getMobileDeviceByDeviceIDTx(ctx context.Context, tx *sql.Tx, deviceID string) (MobileDevice, error) {
	return scanMobileDevice(tx.QueryRowContext(ctx, mobileDeviceSelectSQL()+` WHERE device_id = ?`, deviceID))
}

func getMobileSessionByIDTx(ctx context.Context, tx *sql.Tx, id int64) (MobileSession, error) {
	return scanMobileSession(tx.QueryRowContext(ctx, mobileSessionSelectSQL()+` WHERE id = ?`, id))
}

func scanMobileAuthenticatedSession(row interface{ Scan(...any) error }) (MobileAuthenticatedSession, error) {
	var auth MobileAuthenticatedSession
	var deviceLastSeenAt, deviceRevokedAt, sessionRevokedAt sql.NullString
	var deviceRegisteredAt, deviceCreatedAt, deviceUpdatedAt string
	var sessionExpiresAt, sessionCreatedAt, sessionUpdatedAt string
	if err := row.Scan(
		&auth.Device.ID,
		&auth.Device.DeviceID,
		&auth.Device.DeviceName,
		&auth.Device.Status,
		&deviceRegisteredAt,
		&deviceLastSeenAt,
		&deviceRevokedAt,
		&deviceCreatedAt,
		&deviceUpdatedAt,
		&auth.Session.ID,
		&auth.Session.DeviceRowID,
		&auth.Session.TokenSHA256,
		&auth.Session.CSRFSHA256,
		&auth.Session.Status,
		&sessionExpiresAt,
		&sessionRevokedAt,
		&sessionCreatedAt,
		&sessionUpdatedAt,
	); err != nil {
		return MobileAuthenticatedSession{}, err
	}
	var err error
	auth.Device.RegisteredAt, err = parseTime(deviceRegisteredAt)
	if err != nil {
		return MobileAuthenticatedSession{}, err
	}
	auth.Device.LastSeenAt, err = parseNullableTime(deviceLastSeenAt)
	if err != nil {
		return MobileAuthenticatedSession{}, err
	}
	auth.Device.RevokedAt, err = parseNullableTime(deviceRevokedAt)
	if err != nil {
		return MobileAuthenticatedSession{}, err
	}
	auth.Device.CreatedAt, err = parseTime(deviceCreatedAt)
	if err != nil {
		return MobileAuthenticatedSession{}, err
	}
	auth.Device.UpdatedAt, err = parseTime(deviceUpdatedAt)
	if err != nil {
		return MobileAuthenticatedSession{}, err
	}
	auth.Session.ExpiresAt, err = parseTime(sessionExpiresAt)
	if err != nil {
		return MobileAuthenticatedSession{}, err
	}
	auth.Session.RevokedAt, err = parseNullableTime(sessionRevokedAt)
	if err != nil {
		return MobileAuthenticatedSession{}, err
	}
	auth.Session.CreatedAt, err = parseTime(sessionCreatedAt)
	if err != nil {
		return MobileAuthenticatedSession{}, err
	}
	auth.Session.UpdatedAt, err = parseTime(sessionUpdatedAt)
	if err != nil {
		return MobileAuthenticatedSession{}, err
	}
	return auth, nil
}

func scanMobileDevice(row interface{ Scan(...any) error }) (MobileDevice, error) {
	var device MobileDevice
	var registeredAt, createdAt, updatedAt string
	var lastSeenAt, revokedAt sql.NullString
	if err := row.Scan(&device.ID, &device.DeviceID, &device.DeviceName, &device.Status, &registeredAt, &lastSeenAt, &revokedAt, &createdAt, &updatedAt); err != nil {
		return MobileDevice{}, err
	}
	var err error
	device.RegisteredAt, err = parseTime(registeredAt)
	if err != nil {
		return MobileDevice{}, err
	}
	device.LastSeenAt, err = parseNullableTime(lastSeenAt)
	if err != nil {
		return MobileDevice{}, err
	}
	device.RevokedAt, err = parseNullableTime(revokedAt)
	if err != nil {
		return MobileDevice{}, err
	}
	device.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return MobileDevice{}, err
	}
	device.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return MobileDevice{}, err
	}
	return device, nil
}

func scanMobileSession(row interface{ Scan(...any) error }) (MobileSession, error) {
	var session MobileSession
	var expiresAt, createdAt, updatedAt string
	var revokedAt sql.NullString
	if err := row.Scan(&session.ID, &session.DeviceRowID, &session.TokenSHA256, &session.CSRFSHA256, &session.Status, &expiresAt, &revokedAt, &createdAt, &updatedAt); err != nil {
		return MobileSession{}, err
	}
	var err error
	session.ExpiresAt, err = parseTime(expiresAt)
	if err != nil {
		return MobileSession{}, err
	}
	session.RevokedAt, err = parseNullableTime(revokedAt)
	if err != nil {
		return MobileSession{}, err
	}
	session.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return MobileSession{}, err
	}
	session.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return MobileSession{}, err
	}
	return session, nil
}

func scanMobilePushSubscription(row interface{ Scan(...any) error }) (MobilePushSubscription, error) {
	var subscription MobilePushSubscription
	var revokedAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&subscription.ID, &subscription.DeviceRowID, &subscription.EndpointSHA256, &subscription.EndpointHost, &subscription.UserAgent, &subscription.Platform, &subscription.Status, &revokedAt, &createdAt, &updatedAt); err != nil {
		return MobilePushSubscription{}, err
	}
	var err error
	subscription.RevokedAt, err = parseNullableTime(revokedAt)
	if err != nil {
		return MobilePushSubscription{}, err
	}
	subscription.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return MobilePushSubscription{}, err
	}
	subscription.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return MobilePushSubscription{}, err
	}
	return subscription, nil
}
