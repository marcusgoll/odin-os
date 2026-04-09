CREATE TABLE IF NOT EXISTS learning_proposals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER,
  proposal_type TEXT NOT NULL,
  scope TEXT NOT NULL,
  target_key TEXT NOT NULL,
  summary TEXT NOT NULL,
  hypothesis TEXT NOT NULL,
  change_payload_json TEXT NOT NULL,
  status TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_learning_proposals_status ON learning_proposals(status, id);
CREATE INDEX IF NOT EXISTS idx_learning_proposals_target ON learning_proposals(proposal_type, scope, target_key, id);

CREATE TABLE IF NOT EXISTS learning_evaluations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  proposal_id INTEGER NOT NULL,
  fixture_key TEXT NOT NULL,
  mode TEXT NOT NULL,
  score REAL NOT NULL,
  baseline_summary_json TEXT NOT NULL,
  candidate_summary_json TEXT NOT NULL,
  result_summary TEXT NOT NULL,
  outcome TEXT NOT NULL,
  recorded_at TEXT NOT NULL,
  FOREIGN KEY(proposal_id) REFERENCES learning_proposals(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_learning_evaluations_proposal_id ON learning_evaluations(proposal_id, id);

CREATE TABLE IF NOT EXISTS learning_promotions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  proposal_id INTEGER NOT NULL,
  proposal_type TEXT NOT NULL,
  scope TEXT NOT NULL,
  target_key TEXT NOT NULL,
  status TEXT NOT NULL,
  supersedes_promotion_id INTEGER,
  promoted_by TEXT NOT NULL,
  promoted_at TEXT NOT NULL,
  rolled_back_by TEXT,
  rolled_back_at TEXT,
  rollback_reason TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(proposal_id) REFERENCES learning_proposals(id) ON DELETE CASCADE,
  FOREIGN KEY(supersedes_promotion_id) REFERENCES learning_promotions(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_learning_promotions_proposal_id ON learning_promotions(proposal_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_learning_promotions_active_target
  ON learning_promotions(proposal_type, scope, target_key)
  WHERE status = 'active';
