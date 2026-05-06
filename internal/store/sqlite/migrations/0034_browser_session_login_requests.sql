CREATE TABLE IF NOT EXISTS browser_session_login_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id INTEGER NOT NULL REFERENCES browser_session_profiles(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  handoff_url TEXT,
  expires_at TEXT NOT NULL,
  completed_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_browser_session_login_requests_session ON browser_session_login_requests(session_id, id);
CREATE INDEX IF NOT EXISTS idx_browser_session_login_requests_status ON browser_session_login_requests(status, id);
