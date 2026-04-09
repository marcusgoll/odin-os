package worktrees

import (
	"context"
	"fmt"
	"time"

	"odin-os/internal/store/sqlite"
)

type Git interface {
	RemoveWorktree(context.Context, string, string) error
}

type Manager struct {
	Store *sqlite.Store
	Git   Git
}

type CleanupResult struct {
	Removed []sqlite.WorktreeLease
}

func (manager Manager) Cleanup(ctx context.Context, staleBefore time.Time) (CleanupResult, error) {
	if manager.Store == nil {
		return CleanupResult{}, fmt.Errorf("cleanup store is required")
	}
	if manager.Git == nil {
		return CleanupResult{}, fmt.Errorf("cleanup git adapter is required")
	}

	leases, err := manager.Store.ListCleanupEligibleWorktreeLeases(ctx, staleBefore)
	if err != nil {
		return CleanupResult{}, err
	}

	result := CleanupResult{}
	for _, lease := range leases {
		if err := manager.Git.RemoveWorktree(ctx, lease.RepoRoot, lease.WorktreePath); err != nil {
			return CleanupResult{}, err
		}
		updated, err := manager.Store.MarkWorktreeLeaseCleanedUp(ctx, lease.ID)
		if err != nil {
			return CleanupResult{}, err
		}
		result.Removed = append(result.Removed, updated)
	}

	return result, nil
}
