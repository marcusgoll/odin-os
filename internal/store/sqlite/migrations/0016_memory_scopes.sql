CREATE TABLE IF NOT EXISTS memory_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  initiative_id INTEGER REFERENCES initiatives(id) ON DELETE CASCADE,
  companion_id INTEGER REFERENCES companions(id) ON DELETE CASCADE,
  task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  run_id INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  entry_type TEXT NOT NULL,
  visibility_scope TEXT NOT NULL,
  retention_class TEXT NOT NULL,
  summary TEXT NOT NULL,
  content TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_memory_entries_workspace_visibility_created
  ON memory_entries(workspace_id, visibility_scope, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_memory_entries_initiative_created
  ON memory_entries(initiative_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_memory_entries_companion_created
  ON memory_entries(companion_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_memory_entries_run_created
  ON memory_entries(run_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_memory_entries_task_created
  ON memory_entries(task_id, created_at DESC, id DESC);
