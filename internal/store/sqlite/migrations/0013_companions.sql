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

INSERT INTO companions (
  workspace_id,
  key,
  title,
  kind,
  charter,
  status,
  initiative_scope_json,
  tool_policy_json,
  memory_policy_json,
  planning_policy_json,
  created_at,
  updated_at
)
SELECT
  w.id,
  w.default_companion_key,
  CASE
    WHEN w.default_companion_key = 'primary' THEN 'Primary Assistant'
    ELSE 'Default Companion'
  END,
  'assistant',
  'Default companion for this workspace.',
  'active',
  '{}',
  '{}',
  '{}',
  '{}',
  STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'),
  STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM workspaces w
WHERE NOT EXISTS (
  SELECT 1
  FROM companions existing
  WHERE existing.workspace_id = w.id
    AND existing.key = w.default_companion_key
);
