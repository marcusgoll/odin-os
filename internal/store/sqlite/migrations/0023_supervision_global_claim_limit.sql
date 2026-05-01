CREATE UNIQUE INDEX IF NOT EXISTS idx_supervision_dispatch_claims_one_active_global
ON supervision_dispatch_claims((1))
WHERE status IN ('active', 'reserved');
