CREATE TABLE IF NOT EXISTS browser_session_profiles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  domain TEXT NOT NULL,
  account_hint TEXT NOT NULL DEFAULT '',
  permission_tier TEXT NOT NULL,
  status TEXT NOT NULL,
  profile_path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_verified_at TEXT,
  expires_at TEXT,
  revoked_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_browser_session_profiles_name ON browser_session_profiles(name);
CREATE INDEX IF NOT EXISTS idx_browser_session_profiles_status ON browser_session_profiles(status, id);
CREATE INDEX IF NOT EXISTS idx_browser_session_profiles_domain ON browser_session_profiles(domain, id);
