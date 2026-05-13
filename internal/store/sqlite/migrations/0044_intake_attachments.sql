CREATE TABLE IF NOT EXISTS intake_attachments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  intake_item_id INTEGER NOT NULL REFERENCES intake_items(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  filename TEXT NOT NULL,
  content_type TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  status TEXT NOT NULL,
  bytes BLOB NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_intake_attachments_intake_item
  ON intake_attachments(intake_item_id, id);
