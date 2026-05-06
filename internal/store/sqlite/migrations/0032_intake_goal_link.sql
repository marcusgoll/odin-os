ALTER TABLE intake_items ADD COLUMN goal_id INTEGER REFERENCES goals(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_intake_items_goal ON intake_items(goal_id);
