ALTER TABLE tasks ADD COLUMN acceptance_criteria_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE external_issues ADD COLUMN acceptance_criteria_json TEXT NOT NULL DEFAULT '[]';
