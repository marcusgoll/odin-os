CREATE TABLE IF NOT EXISTS notification_devices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  device_key TEXT NOT NULL,
  label TEXT NOT NULL,
  endpoint_hash TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  user_agent TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  revoked_at TEXT,
  revoke_reason TEXT
);

CREATE INDEX IF NOT EXISTS idx_notification_devices_workspace_key ON notification_devices(workspace_id, device_key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_devices_workspace_endpoint ON notification_devices(workspace_id, endpoint_hash);
CREATE INDEX IF NOT EXISTS idx_notification_devices_workspace_status ON notification_devices(workspace_id, status, id);

CREATE TABLE IF NOT EXISTS notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  source_event_id INTEGER REFERENCES events(id) ON DELETE SET NULL,
  notification_type TEXT NOT NULL,
  priority TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  route TEXT NOT NULL,
  status TEXT NOT NULL,
  push_payload_json TEXT NOT NULL DEFAULT '',
  suppression_reason TEXT NOT NULL DEFAULT '',
  read_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_source_type ON notifications(source_event_id, notification_type) WHERE source_event_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_notifications_workspace_status ON notifications(workspace_id, status, id);
CREATE INDEX IF NOT EXISTS idx_notifications_workspace_created ON notifications(workspace_id, created_at DESC, id DESC);
