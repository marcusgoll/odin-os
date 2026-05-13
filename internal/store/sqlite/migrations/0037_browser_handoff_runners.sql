CREATE TABLE IF NOT EXISTS browser_handoff_runners (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    login_request_id INTEGER NOT NULL,
    handoff_id TEXT NOT NULL,
    status TEXT NOT NULL,
    viewer_url TEXT,
    runner_id TEXT,
    process_id INTEGER,
    bind_addr TEXT,
    private_base_url TEXT,
    public_base_url TEXT,
    expires_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT,
    cancelled_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    error_code TEXT,
    error_message TEXT,
    FOREIGN KEY(session_id) REFERENCES browser_session_profiles(id),
    FOREIGN KEY(login_request_id) REFERENCES browser_session_login_requests(id)
);

CREATE INDEX IF NOT EXISTS idx_browser_handoff_runners_login_request_id ON browser_handoff_runners(login_request_id);
CREATE INDEX IF NOT EXISTS idx_browser_handoff_runners_handoff_id ON browser_handoff_runners(handoff_id);
CREATE INDEX IF NOT EXISTS idx_browser_handoff_runners_status ON browser_handoff_runners(status);
