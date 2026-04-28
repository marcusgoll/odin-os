CREATE TABLE IF NOT EXISTS intake_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL,
  source_family TEXT NOT NULL,
  external_object_id TEXT NOT NULL DEFAULT '',
  event_kind TEXT NOT NULL,
  subject TEXT NOT NULL,
  dedupe_key TEXT NOT NULL,
  dedupe_recipe_version TEXT NOT NULL,
  source_facts_json TEXT NOT NULL,
  status TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT '',
  scope_key TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  conversation_transcript_id INTEGER REFERENCES conversation_transcripts(id) ON DELETE SET NULL,
  canonical_intake_item_id INTEGER REFERENCES intake_items(id) ON DELETE SET NULL,
  suppression_reason TEXT NOT NULL DEFAULT '',
  routing_notes TEXT NOT NULL DEFAULT '',
  received_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_intake_items_workspace_status ON intake_items(workspace_id, status, id);
CREATE INDEX IF NOT EXISTS idx_intake_items_scope ON intake_items(scope, scope_key, id);
CREATE INDEX IF NOT EXISTS idx_intake_items_dedupe_key ON intake_items(workspace_id, dedupe_key, id);
CREATE INDEX IF NOT EXISTS idx_intake_items_canonical ON intake_items(canonical_intake_item_id);
