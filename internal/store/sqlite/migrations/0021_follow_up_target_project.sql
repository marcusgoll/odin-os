ALTER TABLE follow_up_obligations ADD COLUMN target_project_id INTEGER REFERENCES projects(id) ON DELETE RESTRICT;

UPDATE follow_up_obligations
SET target_project_id = COALESCE(
  (SELECT linked_project_id FROM initiatives WHERE initiatives.id = follow_up_obligations.initiative_id),
  (SELECT id FROM projects WHERE key = 'odin-core')
)
WHERE target_project_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_follow_up_obligations_target_project_id ON follow_up_obligations(target_project_id);
