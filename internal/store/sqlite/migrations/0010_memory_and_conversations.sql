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

CREATE TABLE IF NOT EXISTS conversation_transcripts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER,
  task_id INTEGER,
  run_id INTEGER,
  scope TEXT NOT NULL,
  scope_key TEXT NOT NULL,
  mode TEXT NOT NULL,
  prompt TEXT NOT NULL,
  response TEXT NOT NULL,
  tool_summary TEXT NOT NULL DEFAULT '',
  executor TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE SET NULL,
  FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE SET NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_conversation_transcripts_scope
  ON conversation_transcripts(scope, scope_key, id);
CREATE INDEX IF NOT EXISTS idx_conversation_transcripts_project
  ON conversation_transcripts(project_id, id);
CREATE INDEX IF NOT EXISTS idx_conversation_transcripts_task_run
  ON conversation_transcripts(task_id, run_id, id);

CREATE TABLE IF NOT EXISTS memory_summaries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER,
  source_transcript_id INTEGER,
  task_id INTEGER,
  run_id INTEGER,
  scope TEXT NOT NULL,
  scope_key TEXT NOT NULL,
  memory_type TEXT NOT NULL,
  summary TEXT NOT NULL,
  details_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE SET NULL,
  FOREIGN KEY(source_transcript_id) REFERENCES conversation_transcripts(id) ON DELETE SET NULL,
  FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE SET NULL,
  FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_memory_summaries_scope
  ON memory_summaries(scope, scope_key, id);
CREATE INDEX IF NOT EXISTS idx_memory_summaries_project
  ON memory_summaries(project_id, memory_type, id);
CREATE INDEX IF NOT EXISTS idx_memory_summaries_source
  ON memory_summaries(source_transcript_id, id);
