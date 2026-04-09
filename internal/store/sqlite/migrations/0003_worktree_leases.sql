CREATE TABLE IF NOT EXISTS worktree_leases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  mode TEXT NOT NULL,
  branch_name TEXT NOT NULL,
  worktree_path TEXT NOT NULL,
  repo_root TEXT NOT NULL,
  state TEXT NOT NULL,
  heartbeat_at TEXT NOT NULL,
  released_at TEXT,
  cleaned_up_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_worktree_leases_active_task
ON worktree_leases(project_id, task_id)
WHERE mode = 'mutable' AND state = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS idx_worktree_leases_active_branch
ON worktree_leases(branch_name)
WHERE mode = 'mutable' AND state = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS idx_worktree_leases_active_path
ON worktree_leases(worktree_path)
WHERE mode = 'mutable' AND state = 'active';

CREATE INDEX IF NOT EXISTS idx_worktree_leases_task_run ON worktree_leases(task_id, run_id, id);
CREATE INDEX IF NOT EXISTS idx_worktree_leases_cleanup ON worktree_leases(state, heartbeat_at, released_at, cleaned_up_at, id);
