CREATE INDEX IF NOT EXISTS idx_context_packets_scope_status_id
ON context_packets(packet_scope, status, id);
