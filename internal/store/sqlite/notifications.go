package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	runtimeevents "odin-os/internal/runtime/events"
)

func (store *Store) UpsertNotificationDevice(ctx context.Context, params UpsertNotificationDeviceParams) (NotificationDevice, error) {
	now := store.now()
	workspaceID := params.WorkspaceID
	deviceKey := strings.TrimSpace(params.DeviceKey)
	label := strings.TrimSpace(params.Label)
	endpoint := strings.TrimSpace(params.Endpoint)
	p256dh := strings.TrimSpace(params.P256DH)
	auth := strings.TrimSpace(params.Auth)
	if workspaceID <= 0 {
		return NotificationDevice{}, fmt.Errorf("notification workspace_id is required")
	}
	if deviceKey == "" {
		return NotificationDevice{}, fmt.Errorf("notification device_key is required")
	}
	if label == "" {
		label = deviceKey
	}
	if endpoint == "" || p256dh == "" || auth == "" {
		return NotificationDevice{}, fmt.Errorf("notification subscription endpoint and keys are required")
	}
	endpointHash := notificationEndpointHash(endpoint)

	var device NotificationDevice
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := store.getWorkspaceTx(ctx, tx, workspaceID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notification_devices (
				workspace_id, device_key, label, endpoint_hash, endpoint, p256dh, auth, user_agent,
				status, created_at, updated_at, last_seen_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)
			ON CONFLICT(workspace_id, endpoint_hash) DO UPDATE SET
				device_key = excluded.device_key,
				label = excluded.label,
				endpoint = excluded.endpoint,
				p256dh = excluded.p256dh,
				auth = excluded.auth,
				user_agent = excluded.user_agent,
				status = 'active',
				updated_at = excluded.updated_at,
				last_seen_at = excluded.last_seen_at,
				revoked_at = NULL,
				revoke_reason = NULL
		`,
			workspaceID,
			deviceKey,
			label,
			endpointHash,
			endpoint,
			p256dh,
			auth,
			strings.TrimSpace(params.UserAgent),
			formatTime(now),
			formatTime(now),
			formatTime(now),
		); err != nil {
			return err
		}

		record, err := scanNotificationDevice(tx.QueryRowContext(ctx, `
			SELECT id, workspace_id, device_key, label, endpoint_hash, user_agent, status, created_at, updated_at, last_seen_at, revoked_at, revoke_reason
			FROM notification_devices
			WHERE workspace_id = ? AND endpoint_hash = ?
		`, workspaceID, endpointHash))
		if err != nil {
			return err
		}
		device = record
		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamNotification,
			StreamID:   device.ID,
			EventType:  runtimeevents.EventNotificationDeviceSubscribed,
			Scope:      "workspace",
			Payload: runtimeevents.NotificationDeviceSubscribedPayload{
				WorkspaceID:  device.WorkspaceID,
				DeviceID:     device.ID,
				DeviceKey:    device.DeviceKey,
				Label:        device.Label,
				EndpointHash: device.EndpointHash,
				Status:       device.Status,
			},
			OccurredAt: now,
		})
	})
	return device, err
}

func (store *Store) ListNotificationDevices(ctx context.Context, params ListNotificationDevicesParams) ([]NotificationDevice, error) {
	query := `
		SELECT id, workspace_id, device_key, label, endpoint_hash, user_agent, status, created_at, updated_at, last_seen_at, revoked_at, revoke_reason
		FROM notification_devices
		WHERE workspace_id = ?
	`
	args := []any{params.WorkspaceID}
	if !params.IncludeRevoked {
		query += ` AND status != 'revoked'`
	}
	query += ` ORDER BY updated_at DESC, id DESC`

	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []NotificationDevice
	for rows.Next() {
		device, err := scanNotificationDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (store *Store) RevokeNotificationDevice(ctx context.Context, params RevokeNotificationDeviceParams) (NotificationDevice, error) {
	now := store.now()
	reason := strings.TrimSpace(params.Reason)
	if reason == "" {
		reason = "operator requested unsubscribe"
	}
	var device NotificationDevice
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE notification_devices
			SET status = 'revoked', revoked_at = ?, revoke_reason = ?, updated_at = ?
			WHERE workspace_id = ? AND id = ?
		`, formatTime(now), reason, formatTime(now), params.WorkspaceID, params.DeviceID); err != nil {
			return err
		}
		record, err := scanNotificationDevice(tx.QueryRowContext(ctx, `
			SELECT id, workspace_id, device_key, label, endpoint_hash, user_agent, status, created_at, updated_at, last_seen_at, revoked_at, revoke_reason
			FROM notification_devices
			WHERE workspace_id = ? AND id = ?
		`, params.WorkspaceID, params.DeviceID))
		if err != nil {
			return err
		}
		device = record
		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamNotification,
			StreamID:   device.ID,
			EventType:  runtimeevents.EventNotificationDeviceRevoked,
			Scope:      "workspace",
			Payload: runtimeevents.NotificationDeviceRevokedPayload{
				WorkspaceID: device.WorkspaceID,
				DeviceID:    device.ID,
				DeviceKey:   device.DeviceKey,
				Reason:      reason,
			},
			OccurredAt: now,
		})
	})
	return device, err
}

func (store *Store) CreateNotification(ctx context.Context, params CreateNotificationParams) (Notification, error) {
	now := store.now()
	var notification Notification
	err := store.withTx(ctx, func(tx *sql.Tx) error {
		if params.SourceEventID != nil {
			existing, err := scanNotification(tx.QueryRowContext(ctx, `
				SELECT id, workspace_id, source_event_id, notification_type, priority, title, body, route, status, push_payload_json, suppression_reason, read_at, created_at, updated_at
				FROM notifications
				WHERE source_event_id = ? AND notification_type = ?
			`, *params.SourceEventID, strings.TrimSpace(params.NotificationType)))
			if err == nil {
				notification = existing
				return nil
			}
			if err != sql.ErrNoRows {
				return err
			}
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO notifications (
				workspace_id, source_event_id, notification_type, priority, title, body, route, status,
				push_payload_json, suppression_reason, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			params.WorkspaceID,
			nullInt64(params.SourceEventID),
			strings.TrimSpace(params.NotificationType),
			strings.TrimSpace(params.Priority),
			strings.TrimSpace(params.Title),
			strings.TrimSpace(params.Body),
			strings.TrimSpace(params.Route),
			strings.TrimSpace(params.Status),
			strings.TrimSpace(params.PushPayloadJSON),
			strings.TrimSpace(params.SuppressionReason),
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}
		notificationID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		record, err := scanNotification(tx.QueryRowContext(ctx, `
			SELECT id, workspace_id, source_event_id, notification_type, priority, title, body, route, status, push_payload_json, suppression_reason, read_at, created_at, updated_at
			FROM notifications
			WHERE id = ?
		`, notificationID))
		if err != nil {
			return err
		}
		notification = record
		return appendEventTx(ctx, tx, eventInsert{
			StreamType: runtimeevents.StreamNotification,
			StreamID:   notification.ID,
			EventType:  runtimeevents.EventNotificationCreated,
			Scope:      "workspace",
			Payload: runtimeevents.NotificationCreatedPayload{
				WorkspaceID:       notification.WorkspaceID,
				NotificationID:    notification.ID,
				SourceEventID:     notification.SourceEventID,
				NotificationType:  notification.NotificationType,
				Priority:          notification.Priority,
				Route:             notification.Route,
				Status:            notification.Status,
				SuppressionReason: notification.SuppressionReason,
			},
			OccurredAt: now,
		})
	})
	return notification, err
}

func (store *Store) ListNotifications(ctx context.Context, params ListNotificationsParams) ([]Notification, error) {
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `
		SELECT id, workspace_id, source_event_id, notification_type, priority, title, body, route, status, push_payload_json, suppression_reason, read_at, created_at, updated_at
		FROM notifications
		WHERE workspace_id = ?
	`
	args := []any{params.WorkspaceID}
	if params.UnreadOnly {
		query += ` AND read_at IS NULL`
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notifications []Notification
	for rows.Next() {
		notification, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		notifications = append(notifications, notification)
	}
	return notifications, rows.Err()
}

func notificationEndpointHash(endpoint string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(endpoint)))
	return hex.EncodeToString(sum[:])
}

func scanNotificationDevice(row interface{ Scan(...any) error }) (NotificationDevice, error) {
	var device NotificationDevice
	var createdAt, updatedAt, lastSeenAt string
	var revokedAt sql.NullString
	var revokeReason sql.NullString
	if err := row.Scan(
		&device.ID,
		&device.WorkspaceID,
		&device.DeviceKey,
		&device.Label,
		&device.EndpointHash,
		&device.UserAgent,
		&device.Status,
		&createdAt,
		&updatedAt,
		&lastSeenAt,
		&revokedAt,
		&revokeReason,
	); err != nil {
		return NotificationDevice{}, err
	}
	var err error
	if device.CreatedAt, err = parseTime(createdAt); err != nil {
		return NotificationDevice{}, err
	}
	if device.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return NotificationDevice{}, err
	}
	if device.LastSeenAt, err = parseTime(lastSeenAt); err != nil {
		return NotificationDevice{}, err
	}
	if revokedAt.Valid && strings.TrimSpace(revokedAt.String) != "" {
		parsed, err := parseTime(revokedAt.String)
		if err != nil {
			return NotificationDevice{}, err
		}
		device.RevokedAt = &parsed
	}
	if revokeReason.Valid {
		device.RevokeReason = revokeReason.String
	}
	return device, nil
}

func scanNotification(row interface{ Scan(...any) error }) (Notification, error) {
	var notification Notification
	var sourceEventID sql.NullInt64
	var readAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&notification.ID,
		&notification.WorkspaceID,
		&sourceEventID,
		&notification.NotificationType,
		&notification.Priority,
		&notification.Title,
		&notification.Body,
		&notification.Route,
		&notification.Status,
		&notification.PushPayloadJSON,
		&notification.SuppressionReason,
		&readAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Notification{}, err
	}
	if sourceEventID.Valid {
		notification.SourceEventID = &sourceEventID.Int64
	}
	if readAt.Valid && strings.TrimSpace(readAt.String) != "" {
		parsed, err := parseTime(readAt.String)
		if err != nil {
			return Notification{}, err
		}
		notification.ReadAt = &parsed
	}
	var err error
	if notification.CreatedAt, err = parseTime(createdAt); err != nil {
		return Notification{}, err
	}
	if notification.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Notification{}, err
	}
	return notification, nil
}
