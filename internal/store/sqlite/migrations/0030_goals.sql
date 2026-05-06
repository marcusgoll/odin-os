CREATE TABLE IF NOT EXISTS goals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  current_run_id INTEGER REFERENCES goal_runs(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS goal_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  goal_id INTEGER NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  executor TEXT NOT NULL DEFAULT '',
  attempt INTEGER NOT NULL DEFAULT 1,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  summary TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS goal_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  goal_id INTEGER NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  goal_run_id INTEGER REFERENCES goal_runs(id) ON DELETE SET NULL,
  event_type TEXT NOT NULL,
  previous_status TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}',
  occurred_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS goal_blockers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  goal_id INTEGER NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  blocker_type TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL,
  details_json TEXT NOT NULL DEFAULT '{}',
  created_by TEXT NOT NULL DEFAULT '',
  resolved_by TEXT NOT NULL DEFAULT '',
  resolved_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS goal_evidence (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  goal_id INTEGER NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
  goal_run_id INTEGER REFERENCES goal_runs(id) ON DELETE SET NULL,
  evidence_type TEXT NOT NULL,
  summary TEXT NOT NULL,
  uri TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}',
  created_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_goals_status ON goals(status, id);
CREATE INDEX IF NOT EXISTS idx_goal_runs_goal ON goal_runs(goal_id, id);
CREATE INDEX IF NOT EXISTS idx_goal_runs_status ON goal_runs(status, id);
CREATE INDEX IF NOT EXISTS idx_goal_events_goal ON goal_events(goal_id, id);
CREATE INDEX IF NOT EXISTS idx_goal_blockers_goal_status ON goal_blockers(goal_id, status, id);
CREATE INDEX IF NOT EXISTS idx_goal_evidence_goal ON goal_evidence(goal_id, id);
