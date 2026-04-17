CREATE TABLE IF NOT EXISTS follow_up_obligations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  initiative_id INTEGER REFERENCES initiatives(id) ON DELETE SET NULL,
  companion_id INTEGER REFERENCES companions(id) ON DELETE SET NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  cadence_json TEXT NOT NULL,
  next_due_at TEXT NOT NULL,
  last_materialized_at TEXT,
  last_completed_at TEXT,
  policy_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

ALTER TABLE tasks ADD COLUMN follow_up_obligation_id INTEGER REFERENCES follow_up_obligations(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN follow_up_occurrence_key TEXT;

CREATE INDEX IF NOT EXISTS idx_follow_up_obligations_workspace_id ON follow_up_obligations(workspace_id);
CREATE INDEX IF NOT EXISTS idx_follow_up_obligations_next_due_at ON follow_up_obligations(status, next_due_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_follow_up_occurrence
  ON tasks(follow_up_obligation_id, follow_up_occurrence_key)
  WHERE follow_up_obligation_id IS NOT NULL AND follow_up_occurrence_key IS NOT NULL;
