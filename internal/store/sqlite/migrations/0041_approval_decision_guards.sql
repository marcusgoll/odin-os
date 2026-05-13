ALTER TABLE approvals ADD COLUMN policy_snapshot_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE approvals ADD COLUMN runtime_snapshot_hash TEXT NOT NULL DEFAULT '';
