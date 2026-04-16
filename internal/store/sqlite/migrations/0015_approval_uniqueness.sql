CREATE UNIQUE INDEX IF NOT EXISTS idx_approvals_task_pending
ON approvals(task_id)
WHERE status = 'pending';
