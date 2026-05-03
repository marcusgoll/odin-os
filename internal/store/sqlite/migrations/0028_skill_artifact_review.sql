ALTER TABLE skill_artifacts ADD COLUMN review_decision TEXT NOT NULL DEFAULT '';
ALTER TABLE skill_artifacts ADD COLUMN reviewed_at TEXT;
ALTER TABLE skill_artifacts ADD COLUMN reviewed_by TEXT NOT NULL DEFAULT '';
ALTER TABLE skill_artifacts ADD COLUMN review_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE skill_artifacts ADD COLUMN follow_on_task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL;
ALTER TABLE skill_artifacts ADD COLUMN follow_on_task_key TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_skill_artifacts_follow_on_task ON skill_artifacts(follow_on_task_id);
