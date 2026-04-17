ALTER TABLE conversation_transcripts
  ADD COLUMN workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL;
ALTER TABLE conversation_transcripts
  ADD COLUMN initiative_id INTEGER REFERENCES initiatives(id) ON DELETE SET NULL;
ALTER TABLE conversation_transcripts
  ADD COLUMN companion_id INTEGER REFERENCES companions(id) ON DELETE SET NULL;

ALTER TABLE memory_summaries
  ADD COLUMN workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL;
ALTER TABLE memory_summaries
  ADD COLUMN initiative_id INTEGER REFERENCES initiatives(id) ON DELETE SET NULL;
ALTER TABLE memory_summaries
  ADD COLUMN companion_id INTEGER REFERENCES companions(id) ON DELETE SET NULL;
ALTER TABLE memory_summaries
  ADD COLUMN visibility_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE memory_summaries
  ADD COLUMN retention_class TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_conversation_transcripts_workspace
  ON conversation_transcripts(workspace_id, id);
CREATE INDEX IF NOT EXISTS idx_conversation_transcripts_initiative
  ON conversation_transcripts(initiative_id, id);
CREATE INDEX IF NOT EXISTS idx_conversation_transcripts_companion
  ON conversation_transcripts(companion_id, id);

CREATE INDEX IF NOT EXISTS idx_memory_summaries_workspace
  ON memory_summaries(workspace_id, memory_type, id);
CREATE INDEX IF NOT EXISTS idx_memory_summaries_initiative
  ON memory_summaries(initiative_id, memory_type, id);
CREATE INDEX IF NOT EXISTS idx_memory_summaries_companion
  ON memory_summaries(companion_id, memory_type, id);
