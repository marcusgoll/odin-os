ALTER TABLE tasks ADD COLUMN workspace_id INTEGER REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE tasks ADD COLUMN initiative_id INTEGER REFERENCES initiatives(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN companion_id INTEGER REFERENCES companions(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN subject_type TEXT NOT NULL DEFAULT 'project';
ALTER TABLE tasks ADD COLUMN subject_key TEXT NOT NULL DEFAULT '';

UPDATE tasks
SET workspace_id = COALESCE(
  (SELECT i.workspace_id
   FROM initiatives i
   WHERE i.linked_project_id = tasks.project_id
   LIMIT 1),
  (SELECT id FROM workspaces WHERE key = 'marcus' LIMIT 1)
)
WHERE workspace_id IS NULL;

UPDATE tasks
SET initiative_id = (
  SELECT i.id
  FROM initiatives i
  WHERE i.linked_project_id = tasks.project_id
  LIMIT 1
)
WHERE initiative_id IS NULL;

UPDATE tasks
SET companion_id = (
  SELECT i.owner_companion_id
  FROM initiatives i
  WHERE i.linked_project_id = tasks.project_id
  LIMIT 1
)
WHERE companion_id IS NULL;

UPDATE tasks
SET subject_type = 'workspace',
    subject_key = COALESCE(
      (SELECT w.key FROM workspaces w WHERE w.id = tasks.workspace_id LIMIT 1),
      'marcus'
    )
WHERE scope = 'new-project';

UPDATE tasks
SET subject_type = 'project',
    subject_key = (
      SELECT p.key
      FROM projects p
      WHERE p.id = tasks.project_id
      LIMIT 1
    )
WHERE scope != 'new-project';

CREATE INDEX IF NOT EXISTS idx_tasks_workspace_id ON tasks(workspace_id);
CREATE INDEX IF NOT EXISTS idx_tasks_initiative_id ON tasks(initiative_id);
CREATE INDEX IF NOT EXISTS idx_tasks_companion_id ON tasks(companion_id);
