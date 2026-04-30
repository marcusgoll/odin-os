package workspace

import "context"

type Lease struct {
	WorkItemID string
	Path       string
	Branch     string
}

// Manager owns worktree allocation and lease recovery for agency workers.
type Manager interface {
	Acquire(ctx context.Context, workItemID string) (Lease, error)
	Release(ctx context.Context, lease Lease) error
}
