package worktrees

import (
	"context"
	"errors"
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

var ErrWorktreeAlreadyRemoved = errors.New("worktree already removed")

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
	var cleanupErr error
	for _, lease := range leases {
		if err := manager.Git.RemoveWorktree(ctx, lease.RepoRoot, lease.WorktreePath); err != nil && !errors.Is(err, ErrWorktreeAlreadyRemoved) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove worktree lease %d: %w", lease.ID, err))
			continue
		}
		updated, err := manager.Store.MarkWorktreeLeaseCleanedUp(ctx, lease.ID)
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("mark cleaned lease %d: %w", lease.ID, err))
			continue
		}
		result.Removed = append(result.Removed, updated)
	}

	return result, cleanupErr
}
