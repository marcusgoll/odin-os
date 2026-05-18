CREATE TABLE IF NOT EXISTS browser_mutation_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  approval_id INTEGER NOT NULL UNIQUE REFERENCES approvals(id) ON DELETE CASCADE,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  action_kind TEXT NOT NULL,
  allowed_domains_json TEXT NOT NULL DEFAULT '[]',
  start_url TEXT NOT NULL,
  browser_session_id INTEGER REFERENCES browser_session_profiles(id) ON DELETE SET NULL,
  payload_json TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_browser_mutation_requests_task
ON browser_mutation_requests(task_id);
