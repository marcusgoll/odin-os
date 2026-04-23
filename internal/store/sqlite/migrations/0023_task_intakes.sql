CREATE TABLE IF NOT EXISTS task_intakes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL,
  source TEXT NOT NULL,
  intake_type TEXT NOT NULL,
  dedup_key TEXT NOT NULL DEFAULT '',
  requested_by TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_task_intakes_source_dedup
  ON task_intakes(source, dedup_key)
  WHERE dedup_key <> '';
