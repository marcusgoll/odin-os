CREATE TABLE IF NOT EXISTS initiatives (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL,
  key TEXT NOT NULL,
  title TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  summary TEXT NOT NULL,
  owner_companion_id INTEGER,
  linked_project_id INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(linked_project_id) REFERENCES projects(id) ON DELETE SET NULL,
  UNIQUE(workspace_id, key)
);

INSERT INTO initiatives (
  workspace_id,
  key,
  title,
  kind,
  status,
  summary,
  owner_companion_id,
  linked_project_id,
  created_at,
  updated_at
)
SELECT
  default_workspace.id,
  p.key,
  p.name,
  'managed_project',
  'active',
  '',
  NULL,
  p.id,
  STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'),
  STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM projects p
JOIN workspaces default_workspace ON default_workspace.key = 'default'
WHERE NOT EXISTS (
  SELECT 1
  FROM initiatives existing
  WHERE existing.workspace_id = default_workspace.id
    AND existing.key = p.key
);

CREATE INDEX IF NOT EXISTS idx_initiatives_workspace_id ON initiatives(workspace_id, id);
CREATE INDEX IF NOT EXISTS idx_initiatives_kind ON initiatives(kind, id);
CREATE INDEX IF NOT EXISTS idx_initiatives_linked_project_id ON initiatives(linked_project_id);
