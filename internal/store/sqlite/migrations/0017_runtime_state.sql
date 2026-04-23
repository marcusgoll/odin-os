CREATE TABLE IF NOT EXISTS runtime_state (
  singleton_key TEXT PRIMARY KEY,
  boot_id TEXT NOT NULL,
  status TEXT NOT NULL,
  pid INTEGER NOT NULL,
  started_at TEXT NOT NULL,
  ready_at TEXT,
  last_heartbeat_at TEXT NOT NULL,
  last_shutdown_reason TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
