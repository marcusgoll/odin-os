CREATE TABLE IF NOT EXISTS project_transitions (
  project_id INTEGER PRIMARY KEY,
  state TEXT NOT NULL,
  controller TEXT NOT NULL,
  limited_actions_json TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  changed_by TEXT NOT NULL,
  changed_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS project_transition_reports (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id INTEGER NOT NULL,
  report_type TEXT NOT NULL,
  summary TEXT NOT NULL,
  details_json TEXT NOT NULL,
  recorded_at TEXT NOT NULL,
  FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_project_transition_reports_project_id ON project_transition_reports(project_id, id);
