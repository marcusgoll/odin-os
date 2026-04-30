CREATE TABLE IF NOT EXISTS external_issues (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  repo TEXT NOT NULL,
  number INTEGER NOT NULL,
  title TEXT NOT NULL,
  body_hash TEXT NOT NULL,
  url TEXT NOT NULL,
  state TEXT NOT NULL,
  labels_json TEXT NOT NULL,
  sync_status TEXT NOT NULL,
  last_synced_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(provider, repo, number)
);

CREATE INDEX IF NOT EXISTS idx_external_issues_project_id ON external_issues(project_id);
CREATE INDEX IF NOT EXISTS idx_external_issues_repo_status ON external_issues(repo, sync_status);
