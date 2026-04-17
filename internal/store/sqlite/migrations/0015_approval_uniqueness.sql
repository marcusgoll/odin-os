UPDATE approvals
SET
  status = 'rejected',
  resolved_at = COALESCE(resolved_at, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')),
  decision_by = CASE
    WHEN decision_by IS NULL OR decision_by = '' THEN 'migration'
    ELSE decision_by
  END,
  reason = CASE
    WHEN reason IS NULL OR reason = '' THEN 'deduplicated before approval uniqueness index'
    ELSE reason
  END
WHERE status = 'pending'
  AND id NOT IN (
    SELECT MAX(id)
    FROM approvals
    WHERE status = 'pending'
    GROUP BY task_id
  );

CREATE UNIQUE INDEX IF NOT EXISTS idx_approvals_task_pending
ON approvals(task_id)
WHERE status = 'pending';
