CREATE TABLE IF NOT EXISTS automation_triggers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL,
  key TEXT NOT NULL,
  project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  initiative_key TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  rule_json TEXT NOT NULL,
  rule_summary TEXT NOT NULL DEFAULT '',
  work_item_title TEXT NOT NULL DEFAULT '',
  next_eligible_at TEXT,
  last_evaluated_at TEXT,
  last_materialized_at TEXT,
  last_materialization_key TEXT NOT NULL DEFAULT '',
  last_work_item_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(workspace_id, key)
);

CREATE TABLE IF NOT EXISTS automation_trigger_materializations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  trigger_id INTEGER NOT NULL REFERENCES automation_triggers(id) ON DELETE CASCADE,
  materialization_key TEXT NOT NULL,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  reason TEXT NOT NULL DEFAULT '',
  requested_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(trigger_id, materialization_key)
);

CREATE INDEX IF NOT EXISTS idx_automation_triggers_workspace_status ON automation_triggers(workspace_id, status, id);
CREATE INDEX IF NOT EXISTS idx_automation_triggers_initiative ON automation_triggers(initiative_key, id);
CREATE INDEX IF NOT EXISTS idx_automation_materializations_trigger ON automation_trigger_materializations(trigger_id, id);
