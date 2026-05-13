CREATE TABLE IF NOT EXISTS pull_request_handoffs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  repo TEXT NOT NULL,
  number INTEGER NOT NULL,
  url TEXT NOT NULL,
  state TEXT NOT NULL,
  issue_url TEXT NOT NULL,
  branch TEXT NOT NULL,
  title TEXT NOT NULL,
  summary TEXT NOT NULL,
  tests_json TEXT NOT NULL DEFAULT '[]',
  risks_json TEXT NOT NULL DEFAULT '[]',
  blockers_json TEXT NOT NULL DEFAULT '[]',
  selected_roles_json TEXT NOT NULL DEFAULT '[]',
  review_state TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(project_id, issue_url, branch, provider, repo, number)
);

CREATE INDEX IF NOT EXISTS idx_pull_request_handoffs_project_id ON pull_request_handoffs(project_id);
CREATE INDEX IF NOT EXISTS idx_pull_request_handoffs_repo_review_state ON pull_request_handoffs(repo, review_state);

CREATE TABLE IF NOT EXISTS pull_request_review_results (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  handoff_id INTEGER NOT NULL REFERENCES pull_request_handoffs(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  state TEXT NOT NULL,
  summary TEXT NOT NULL,
  comments_json TEXT NOT NULL DEFAULT '[]',
  blockers_json TEXT NOT NULL DEFAULT '[]',
  outcome TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(handoff_id, role)
);

CREATE INDEX IF NOT EXISTS idx_pull_request_review_results_handoff_id ON pull_request_review_results(handoff_id);
