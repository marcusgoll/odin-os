CREATE TABLE IF NOT EXISTS skill_artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  skill_key TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT 'repo',
  project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  status TEXT NOT NULL,
  artifact_type TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  output_json TEXT NOT NULL DEFAULT '{}',
  raw_output TEXT NOT NULL DEFAULT '',
  handler_ref TEXT NOT NULL DEFAULT '',
  execution_profile TEXT NOT NULL DEFAULT '',
  permissions_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_skill_artifacts_skill_key ON skill_artifacts(skill_key, id);
CREATE INDEX IF NOT EXISTS idx_skill_artifacts_status ON skill_artifacts(status, id);
CREATE INDEX IF NOT EXISTS idx_skill_artifacts_project ON skill_artifacts(project_id, id);
