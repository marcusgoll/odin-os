ALTER TABLE goal_runs ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE goal_runs ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 1;
ALTER TABLE goal_runs ADD COLUMN last_progress_at TEXT;
ALTER TABLE goal_runs ADD COLUMN next_wake_at TEXT;
ALTER TABLE goal_runs ADD COLUMN ended_at TEXT;
ALTER TABLE goal_runs ADD COLUMN lease_owner TEXT NOT NULL DEFAULT '';

UPDATE goal_runs
SET attempts = attempt
WHERE attempts = 0 AND attempt > 0;

UPDATE goal_runs
SET last_progress_at = started_at
WHERE last_progress_at IS NULL;

UPDATE goal_runs
SET ended_at = finished_at
WHERE ended_at IS NULL AND finished_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_goal_runs_goal_active ON goal_runs(goal_id, ended_at, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_goal_runs_one_active ON goal_runs(goal_id)
WHERE ended_at IS NULL AND status NOT IN ('completed', 'failed', 'canceled');
