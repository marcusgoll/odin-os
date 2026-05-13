CREATE TABLE IF NOT EXISTS mobile_devices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_id TEXT NOT NULL UNIQUE,
  device_name TEXT NOT NULL,
  status TEXT NOT NULL,
  registered_at TEXT NOT NULL,
  last_seen_at TEXT,
  revoked_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mobile_devices_status
  ON mobile_devices(status, id);

CREATE TABLE IF NOT EXISTS mobile_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_row_id INTEGER NOT NULL REFERENCES mobile_devices(id) ON DELETE CASCADE,
  token_sha256 TEXT NOT NULL UNIQUE,
  csrf_sha256 TEXT NOT NULL,
  status TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mobile_sessions_device
  ON mobile_sessions(device_row_id, status, id);

CREATE TABLE IF NOT EXISTS mobile_push_subscriptions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_row_id INTEGER NOT NULL REFERENCES mobile_devices(id) ON DELETE CASCADE,
  endpoint_sha256 TEXT NOT NULL,
  endpoint_host TEXT NOT NULL,
  user_agent TEXT NOT NULL,
  platform TEXT NOT NULL,
  status TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(device_row_id, endpoint_sha256)
);

CREATE INDEX IF NOT EXISTS idx_mobile_push_subscriptions_device
  ON mobile_push_subscriptions(device_row_id, status, id);
