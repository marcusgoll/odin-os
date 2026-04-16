CREATE TABLE IF NOT EXISTS initiatives (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  title TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  summary TEXT NOT NULL,
  linked_project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  owner_companion_id INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(workspace_id, key)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_initiatives_linked_project_id
  ON initiatives(linked_project_id)
  WHERE linked_project_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_initiatives_workspace_id ON initiatives(workspace_id);
CREATE INDEX IF NOT EXISTS idx_initiatives_kind_status ON initiatives(kind, status);
