CREATE TABLE IF NOT EXISTS actions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workflow_key TEXT NOT NULL,
  workflow_run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  action_type TEXT NOT NULL,
  lifecycle_state TEXT NOT NULL,
  current_payload_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS action_payloads (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
  payload_schema TEXT NOT NULL,
  payload_schema_version INTEGER NOT NULL,
  payload_hash TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  submit_path TEXT NOT NULL,
  readback_path TEXT NOT NULL,
  proof_requirement TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(action_id, payload_hash)
);

CREATE TABLE IF NOT EXISTS action_evidence_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  event_version INTEGER NOT NULL,
  payload_hash TEXT,
  approval_id INTEGER REFERENCES approvals(id) ON DELETE SET NULL,
  run_id INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  source TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  occurred_at TEXT NOT NULL
);

ALTER TABLE approvals ADD COLUMN action_id INTEGER REFERENCES actions(id) ON DELETE SET NULL;
ALTER TABLE approvals ADD COLUMN payload_hash TEXT;

CREATE INDEX IF NOT EXISTS idx_actions_workflow_run_id ON actions(workflow_run_id);
CREATE INDEX IF NOT EXISTS idx_actions_workflow_key ON actions(workflow_key);
CREATE INDEX IF NOT EXISTS idx_action_payloads_action_id ON action_payloads(action_id);
CREATE INDEX IF NOT EXISTS idx_action_evidence_action_id ON action_evidence_events(action_id, id);
CREATE INDEX IF NOT EXISTS idx_approvals_action_id ON approvals(action_id);
