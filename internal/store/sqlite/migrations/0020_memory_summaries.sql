CREATE TABLE IF NOT EXISTS conversation_transcripts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  run_id INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  scope TEXT NOT NULL,
  scope_key TEXT NOT NULL,
  mode TEXT NOT NULL,
  prompt TEXT NOT NULL,
  response TEXT NOT NULL,
  tool_summary TEXT NOT NULL,
  executor TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS memory_summaries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  source_transcript_id INTEGER REFERENCES conversation_transcripts(id) ON DELETE SET NULL,
  task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  run_id INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  scope TEXT NOT NULL,
  scope_key TEXT NOT NULL,
  memory_type TEXT NOT NULL,
  summary TEXT NOT NULL,
  details_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_conversation_transcripts_scope ON conversation_transcripts(scope, scope_key, id);
CREATE INDEX IF NOT EXISTS idx_conversation_transcripts_project_id ON conversation_transcripts(project_id, id);
CREATE INDEX IF NOT EXISTS idx_conversation_transcripts_task_id ON conversation_transcripts(task_id, id);
CREATE INDEX IF NOT EXISTS idx_conversation_transcripts_run_id ON conversation_transcripts(run_id, id);

CREATE INDEX IF NOT EXISTS idx_memory_summaries_scope ON memory_summaries(scope, scope_key, memory_type, id);
CREATE INDEX IF NOT EXISTS idx_memory_summaries_project_id ON memory_summaries(project_id, id);
CREATE INDEX IF NOT EXISTS idx_memory_summaries_source_transcript_id ON memory_summaries(source_transcript_id, id);
CREATE INDEX IF NOT EXISTS idx_memory_summaries_task_id ON memory_summaries(task_id, id);
CREATE INDEX IF NOT EXISTS idx_memory_summaries_run_id ON memory_summaries(run_id, id);
