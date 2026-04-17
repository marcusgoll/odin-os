CREATE TABLE IF NOT EXISTS memory_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_type TEXT NOT NULL,
    scope_key TEXT NOT NULL,
    source_scope TEXT NOT NULL,
    visibility_scope TEXT NOT NULL,
    retention_intent TEXT NOT NULL,
    entry_kind TEXT NOT NULL DEFAULT 'note',
    summary TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    source_run_id INTEGER,
    source_ref TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY (source_run_id) REFERENCES runs(id)
);

CREATE INDEX IF NOT EXISTS idx_memory_entries_scope
    ON memory_entries(scope_type, scope_key, id DESC);

CREATE INDEX IF NOT EXISTS idx_memory_entries_source_run
    ON memory_entries(source_run_id);
