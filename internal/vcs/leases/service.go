package leases

import (
	"context"
	"fmt"
	"time"

	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/worktrees"
)

type Maintenance struct {
	Store   *sqlite.Store
	Cleanup worktrees.Manager
	Now     func() time.Time
}

type HeartbeatResult struct {
	Updated int
}

func (maint Maintenance) HeartbeatActive(ctx context.Context) (HeartbeatResult, error) {
	if maint.Store == nil {
		return HeartbeatResult{}, fmt.Errorf("lease maintenance store is required")
	}

	leases, err := maint.Store.ListActiveWorktreeLeases(ctx)
	if err != nil {
		return HeartbeatResult{}, err
	}

	result := HeartbeatResult{}
	for _, lease := range leases {
		if _, err := maint.Store.HeartbeatWorktreeLease(ctx, lease.ID); err != nil {
			return HeartbeatResult{}, err
		}
		result.Updated++
	}

	return result, nil
}

func (maint Maintenance) CleanupExpired(ctx context.Context, staleAfter time.Duration) (worktrees.CleanupResult, error) {
	if maint.Store == nil {
		return worktrees.CleanupResult{}, fmt.Errorf("lease maintenance store is required")
	}
	if staleAfter <= 0 {
		return worktrees.CleanupResult{}, fmt.Errorf("staleAfter must be positive")
	}

	cleanup := maint.Cleanup
	if cleanup.Store == nil {
		cleanup.Store = maint.Store
	}
	if cleanup.Git == nil {
		return worktrees.CleanupResult{}, fmt.Errorf("lease cleanup git adapter is required")
	}

	return cleanup.Cleanup(ctx, maint.now().Add(-staleAfter))
}

func (maint Maintenance) now() time.Time {
	if maint.Now != nil {
		return maint.Now().UTC()
	}
	return time.Now().UTC()
}
