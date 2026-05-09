package leases

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
	"odin-os/internal/vcs/branches"
	"odin-os/internal/vcs/worktrees"
)

type Git interface {
	BranchExists(context.Context, string, string) (bool, error)
	CreateBranch(context.Context, string, string, string) error
	AddWorktree(context.Context, string, string, string) error
	RemoveWorktree(context.Context, string, string) error
	WorktreeDirty(context.Context, string) (bool, error)
}

type Preparer interface {
	Prepare(context.Context, Request) (Assignment, error)
}

type WorkspaceManager = Preparer

type Manager struct {
	Store        *sqlite.Store
	Git          Git
	WorktreeRoot string
	Logger       *logs.Logger
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
		err := fmt.Errorf("lease store is required for mutable work")
		manager.logPrepareFailure(ctx, request, "store_missing", "", "", request.RepoRoot, err)
		return Assignment{}, err
	}
	if manager.Git == nil {
		err := fmt.Errorf("git adapter is required for mutable work")
		manager.logPrepareFailure(ctx, request, "git_missing", "", "", request.RepoRoot, err)
		return Assignment{}, err
	}

	existing, err := manager.Store.GetActiveWorktreeLeaseByTaskRun(ctx, request.TaskID, request.RunID)
	if err == nil {
		updated, heartbeatErr := manager.Store.HeartbeatWorktreeLease(ctx, existing.ID)
		if heartbeatErr != nil {
			manager.logPrepareFailure(ctx, request, "heartbeat_failed", existing.BranchName, existing.WorktreePath, existing.RepoRoot, heartbeatErr)
			return Assignment{}, heartbeatErr
		}
		manager.logPrepared(ctx, request, updated, true)
		return assignmentFromLease(updated, true), nil
	}
	if err != nil && err != sql.ErrNoRows {
		manager.logPrepareFailure(ctx, request, "active_lease_lookup_failed", "", "", request.RepoRoot, err)
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
			manager.logPrepareFailure(ctx, request, "worktree_stat_failed", branchName, worktreePath, request.RepoRoot, statErr)
			return Assignment{}, statErr
		}

		exists, err := manager.Git.BranchExists(ctx, request.RepoRoot, branchName)
		if err != nil {
			manager.logPrepareFailure(ctx, request, "branch_exists_failed", branchName, worktreePath, request.RepoRoot, err)
			return Assignment{}, err
		}
		if !exists {
			if err := manager.Git.CreateBranch(ctx, request.RepoRoot, branchName, request.DefaultBranch); err != nil {
				manager.logPrepareFailure(ctx, request, "create_branch_failed", branchName, worktreePath, request.RepoRoot, err)
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
					manager.logPrepareFailure(ctx, request, "task_lease_conflict", branchName, worktreePath, request.RepoRoot, err)
					return Assignment{}, err
				}
				continue
			}
			manager.logPrepareFailure(ctx, request, "create_lease_failed", branchName, worktreePath, request.RepoRoot, err)
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
			manager.logPrepareFailure(ctx, request, "add_worktree_failed", branchName, worktreePath, request.RepoRoot, err)
			return Assignment{}, err
		}

		manager.logPrepared(ctx, request, lease, false)
		return assignmentFromLease(lease, false), nil
	}

	err = fmt.Errorf("unable to allocate worktree for task %d run %d after %d tries", request.TaskID, request.RunID, maxPrepareTries)
	manager.logPrepareFailure(ctx, request, "allocation_exhausted", "", "", request.RepoRoot, err)
	return Assignment{}, err
}

func (manager Manager) logPrepared(ctx context.Context, request Request, lease sqlite.WorktreeLease, reused bool) {
	outcome := "prepared"
	message := "workspace lease prepared"
	if reused {
		outcome = "reused"
		message = "workspace lease reused"
	}
	leaseID := lease.ID
	manager.logPrepareRecord(ctx, request, logs.LevelInfo, message, outcome, "", &leaseID, lease.BranchName, lease.WorktreePath, lease.RepoRoot, nil)
}

func (manager Manager) logPrepareFailure(ctx context.Context, request Request, reason string, branchName string, worktreePath string, repoRoot string, err error) {
	manager.logPrepareRecord(ctx, request, logs.LevelWarn, "workspace lease prepare failed", "failed", reason, nil, branchName, worktreePath, repoRoot, err)
}

func (manager Manager) logPrepareRecord(ctx context.Context, request Request, level logs.Level, message string, outcome string, reason string, leaseID *int64, branchName string, worktreePath string, repoRoot string, cause error) {
	if manager.Logger == nil {
		return
	}
	fields := map[string]any{
		"operation":   "prepare",
		"outcome":     outcome,
		"project_key": request.ProjectKey,
	}
	if reason != "" {
		fields["reason"] = reason
	}
	if leaseID != nil {
		fields["lease_id"] = *leaseID
	}
	if branchName != "" {
		fields["branch_name"] = branchName
	}
	if worktreePath != "" {
		fields["worktree_path"] = worktreePath
	}
	if repoRoot != "" {
		fields["repo_root"] = repoRoot
	}
	if cause != nil {
		fields["error"] = cause.Error()
	}
	_ = manager.Logger.Log(logs.Record{
		Level:         level,
		Component:     "workspace",
		Message:       message,
		CorrelationID: fmt.Sprintf("task:%d/run:%d", request.TaskID, request.RunID),
		Scope:         "project",
		ProjectID:     int64Ptr(request.ProjectID),
		TaskID:        int64Ptr(request.TaskID),
		RunID:         int64Ptr(request.RunID),
		Fields:        fields,
	})
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
