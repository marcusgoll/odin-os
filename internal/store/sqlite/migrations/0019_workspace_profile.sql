CREATE TABLE IF NOT EXISTS workspace_profile (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id INTEGER NOT NULL UNIQUE REFERENCES workspaces(id) ON DELETE CASCADE,
  preferences_json TEXT NOT NULL,
  boundaries_json TEXT NOT NULL,
  cadence_defaults_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
