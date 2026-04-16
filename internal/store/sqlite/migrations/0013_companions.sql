CREATE TABLE IF NOT EXISTS companions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL,
  key TEXT NOT NULL,
  title TEXT NOT NULL,
  kind TEXT NOT NULL,
  charter TEXT NOT NULL,
  status TEXT NOT NULL,
  initiative_scope_json TEXT NOT NULL,
  tool_policy_json TEXT NOT NULL,
  memory_policy_json TEXT NOT NULL,
  planning_policy_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  UNIQUE(workspace_id, key)
);

CREATE INDEX IF NOT EXISTS idx_companions_workspace_id ON companions(workspace_id, id);
CREATE INDEX IF NOT EXISTS idx_companions_kind ON companions(kind, id);
