CREATE TABLE IF NOT EXISTS companions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  title TEXT NOT NULL,
  kind TEXT NOT NULL,
  charter TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  initiative_scope_json TEXT NOT NULL DEFAULT '{}',
  memory_policy_json TEXT NOT NULL DEFAULT '{}',
  planning_policy_json TEXT NOT NULL DEFAULT '{}',
  tool_policy_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(workspace_id, key)
);

CREATE INDEX IF NOT EXISTS idx_companions_workspace_id ON companions(workspace_id, id);
