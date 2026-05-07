CREATE TABLE IF NOT EXISTS browser_encrypted_profile_artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id INTEGER NOT NULL REFERENCES browser_session_profiles(id) ON DELETE CASCADE,
  profile_path TEXT NOT NULL,
  encrypted_artifact_path TEXT NOT NULL,
  encryption_key_ref TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  expires_at TEXT,
  revoked_at TEXT,
  error_code TEXT,
  error_message TEXT
);

CREATE INDEX IF NOT EXISTS idx_browser_encrypted_profile_artifacts_session ON browser_encrypted_profile_artifacts(session_id, id);
CREATE INDEX IF NOT EXISTS idx_browser_encrypted_profile_artifacts_status ON browser_encrypted_profile_artifacts(status, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_browser_encrypted_profile_artifacts_path ON browser_encrypted_profile_artifacts(encrypted_artifact_path);
