CREATE TABLE IF NOT EXISTS supervision_controls (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  mode_key TEXT NOT NULL,
  status TEXT NOT NULL,
  kill_switch_active INTEGER NOT NULL CHECK (kill_switch_active IN (0, 1)),
  config_hash TEXT NOT NULL,
  max_concurrent_tasks INTEGER NOT NULL,
  dry_run INTEGER NOT NULL CHECK (dry_run IN (0, 1)),
  require_human_approval INTEGER NOT NULL CHECK (require_human_approval IN (0, 1)),
  updated_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_supervision_controls_mode_key
ON supervision_controls(mode_key);

CREATE TABLE IF NOT EXISTS supervision_queue_decisions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  repo TEXT NOT NULL,
  issue_number INTEGER NOT NULL,
  decision TEXT NOT NULL,
  reason TEXT NOT NULL,
  config_hash TEXT NOT NULL,
  decision_json TEXT NOT NULL,
  decided_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(project_id, repo, issue_number)
);

CREATE INDEX IF NOT EXISTS idx_supervision_queue_decisions_project_repo
ON supervision_queue_decisions(project_id, repo, issue_number);

CREATE TABLE IF NOT EXISTS supervision_dispatch_claims (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  repo TEXT NOT NULL,
  issue_number INTEGER NOT NULL,
  claim_key TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  config_hash TEXT NOT NULL,
  claimed_by TEXT NOT NULL,
  claimed_at TEXT NOT NULL,
  released_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_supervision_dispatch_claims_active_issue
ON supervision_dispatch_claims(project_id, repo, issue_number)
WHERE status IN ('active', 'reserved');

CREATE INDEX IF NOT EXISTS idx_supervision_dispatch_claims_project_repo
ON supervision_dispatch_claims(project_id, repo, issue_number, status);

CREATE TABLE IF NOT EXISTS supervision_recovery_observations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
  mode_key TEXT NOT NULL,
  observation_type TEXT NOT NULL,
  status TEXT NOT NULL,
  reason TEXT NOT NULL,
  config_hash TEXT NOT NULL,
  details_json TEXT NOT NULL,
  observed_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_supervision_recovery_observations_project_mode
ON supervision_recovery_observations(project_id, mode_key, observed_at DESC, id DESC);
