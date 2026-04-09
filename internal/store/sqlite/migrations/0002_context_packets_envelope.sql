ALTER TABLE context_packets ADD COLUMN packet_scope TEXT NOT NULL DEFAULT 'task_wake_packet';
ALTER TABLE context_packets ADD COLUMN trigger TEXT NOT NULL DEFAULT 'handoff';
ALTER TABLE context_packets ADD COLUMN checkpoint_key TEXT NOT NULL DEFAULT '';
ALTER TABLE context_packets ADD COLUMN supersedes_packet_id INTEGER REFERENCES context_packets(id) ON DELETE SET NULL;
ALTER TABLE context_packets ADD COLUMN status TEXT NOT NULL DEFAULT 'active';

CREATE INDEX IF NOT EXISTS idx_context_packets_run_id ON context_packets(run_id);
CREATE INDEX IF NOT EXISTS idx_context_packets_scope_status ON context_packets(task_id, packet_scope, status, id);
