CREATE TABLE IF NOT EXISTS delegations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  parent_run_id INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  scope TEXT NOT NULL,
  delegation_key TEXT NOT NULL,
  role TEXT NOT NULL,
  action_class TEXT NOT NULL,
  action_key TEXT NOT NULL,
  mutation_mode TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'queued',
  convergence_mode TEXT NOT NULL,
  artifact_target TEXT NOT NULL DEFAULT '',
  executor TEXT NOT NULL,
  child_task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  child_run_id INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  worktree_lease_id INTEGER REFERENCES worktree_leases(id) ON DELETE SET NULL,
  branch_name TEXT NOT NULL DEFAULT '',
  details_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_delegations_parent_task_id ON delegations(parent_task_id);
CREATE INDEX IF NOT EXISTS idx_delegations_project_id ON delegations(project_id);
CREATE INDEX IF NOT EXISTS idx_delegations_status ON delegations(status);
CREATE INDEX IF NOT EXISTS idx_delegations_child_task_id ON delegations(child_task_id);
CREATE INDEX IF NOT EXISTS idx_delegations_worktree_lease_id ON delegations(worktree_lease_id);

CREATE TABLE IF NOT EXISTS delegation_artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  delegation_id INTEGER NOT NULL REFERENCES delegations(id) ON DELETE CASCADE,
  artifact_type TEXT NOT NULL,
  summary TEXT NOT NULL,
  details_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_delegation_artifacts_delegation_id ON delegation_artifacts(delegation_id);
