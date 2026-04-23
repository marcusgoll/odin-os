package leases

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/branches"
	"odin-os/internal/vcs/worktrees"
)

type Git interface {
	BranchExists(context.Context, string, string) (bool, error)
	CreateBranch(context.Context, string, string, string) error
	AddWorktree(context.Context, string, string, string) error
	RemoveWorktree(context.Context, string, string) error
}

type Manager struct {
	Store        *sqlite.Store
	Git          Git
	WorktreeRoot string
}

type Request struct {
	Mutating      bool
	ProjectID     int64
	ProjectKey    string
	TaskID        int64
	RunID         int64
	RepoRoot      string
	DefaultBranch string
	Try           int
}

type Assignment struct {
	Mode         string
	LeaseID      *int64
	BranchName   string
	WorktreePath string
	RepoRoot     string
	Reused       bool
}

func (manager Manager) Prepare(ctx context.Context, request Request) (Assignment, error) {
	if !request.Mutating {
		return Assignment{
			Mode:         "read_only",
			WorktreePath: request.RepoRoot,
			RepoRoot:     request.RepoRoot,
		}, nil
	}
	if manager.Store == nil {
		return Assignment{}, fmt.Errorf("lease store is required for mutable work")
	}
	if manager.Git == nil {
		return Assignment{}, fmt.Errorf("git adapter is required for mutable work")
	}

	existing, err := manager.Store.GetActiveWorktreeLeaseByTaskRun(ctx, request.TaskID, request.RunID)
	if err == nil {
		updated, heartbeatErr := manager.Store.HeartbeatWorktreeLease(ctx, existing.ID)
		if heartbeatErr != nil {
			return Assignment{}, heartbeatErr
		}
		return assignmentFromLease(updated, true), nil
	}
	if err != nil && err != sql.ErrNoRows {
		return Assignment{}, err
	}

	baseTry := request.Try
	if baseTry <= 0 {
		baseTry = 1
	}

	const maxPrepareTries = 20
	for try := baseTry; try < baseTry+maxPrepareTries; try++ {
		branchName := branches.Name(branches.NameParams{
			ProjectKey: request.ProjectKey,
			TaskID:     request.TaskID,
			RunID:      request.RunID,
			Try:        try,
		})
		worktreePath := worktrees.ResolvePath(worktrees.PathParams{
			Root:       manager.WorktreeRoot,
			ProjectKey: request.ProjectKey,
			TaskID:     request.TaskID,
			RunID:      request.RunID,
			Try:        try,
		})

		if _, statErr := os.Stat(worktreePath); statErr == nil {
			continue
		} else if !os.IsNotExist(statErr) {
			return Assignment{}, statErr
		}

		exists, err := manager.Git.BranchExists(ctx, request.RepoRoot, branchName)
		if err != nil {
			return Assignment{}, err
		}
		if !exists {
			if err := manager.Git.CreateBranch(ctx, request.RepoRoot, branchName, request.DefaultBranch); err != nil {
				return Assignment{}, err
			}
		}

		lease, err := manager.Store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
			ProjectID:    request.ProjectID,
			TaskID:       request.TaskID,
			RunID:        request.RunID,
			Mode:         "mutable",
			BranchName:   branchName,
			WorktreePath: worktreePath,
			RepoRoot:     request.RepoRoot,
			State:        "active",
		})
		if err != nil {
			if errors.Is(err, sqlite.ErrWorktreeLeaseConflict) {
				if isTaskLeaseConflictError(err) {
					return Assignment{}, err
				}
				continue
			}
			return Assignment{}, err
		}

		if err := manager.Git.AddWorktree(ctx, request.RepoRoot, worktreePath, branchName); err != nil {
			_, _ = manager.Store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
				LeaseID: lease.ID,
				State:   "released",
			})
			if isExistingWorktreePathError(err) {
				continue
			}
			return Assignment{}, err
		}

		return assignmentFromLease(lease, false), nil
	}

	return Assignment{}, fmt.Errorf("unable to allocate worktree for task %d run %d after %d tries", request.TaskID, request.RunID, maxPrepareTries)
}

func assignmentFromLease(lease sqlite.WorktreeLease, reused bool) Assignment {
	return Assignment{
		Mode:         lease.Mode,
		LeaseID:      int64Ptr(lease.ID),
		BranchName:   lease.BranchName,
		WorktreePath: lease.WorktreePath,
		RepoRoot:     lease.RepoRoot,
		Reused:       reused,
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}

func isExistingWorktreePathError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "already exists")
}

func isTaskLeaseConflictError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "idx_worktree_leases_active_task") ||
		strings.Contains(message, "worktree_leases.project_id, worktree_leases.task_id")
}
