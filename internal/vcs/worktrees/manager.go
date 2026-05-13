package worktrees

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
)

type Git interface {
	RemoveWorktree(context.Context, string, string) error
	WorktreeDirty(context.Context, string) (bool, error)
}

type Manager struct {
	Store        *sqlite.Store
	Git          Git
	WorktreeRoot string
	Logger       *logs.Logger
}

var ErrWorktreeAlreadyRemoved = errors.New("worktree already removed")
var ErrDirtyWorktree = errors.New("dirty worktree requires explicit force cleanup")

type CleanupResult struct {
	Removed []sqlite.WorktreeLease
}

const (
	CleanupActionCleanup = "cleanup"
	CleanupActionRefuse  = "refuse"
	CleanupActionSkip    = "skip"
)

type CleanupOptions struct {
	ForceDirty     bool
	ApprovedBy     string
	ApprovalReason string
}

type CleanupPreview struct {
	Leases []CleanupPreviewLease
}

type CleanupPreviewLease struct {
	Lease  sqlite.WorktreeLease
	Action string
	Reason string
	Dirty  *bool
	Error  string
}

func (manager Manager) Cleanup(ctx context.Context, staleBefore time.Time) (CleanupResult, error) {
	if manager.Store == nil {
		return CleanupResult{}, fmt.Errorf("cleanup store is required")
	}
	if manager.Git == nil {
		return CleanupResult{}, fmt.Errorf("cleanup git adapter is required")
	}
	root, err := cleanupRoot(manager.WorktreeRoot)
	if err != nil {
		return CleanupResult{}, err
	}

	leases, err := manager.Store.ListCleanupEligibleWorktreeLeases(ctx, staleBefore)
	if err != nil {
		return CleanupResult{}, err
	}

	return manager.cleanupLeases(ctx, root, leases, CleanupOptions{})
}

func (manager Manager) PreviewCleanup(ctx context.Context, staleBefore time.Time) (CleanupPreview, error) {
	if manager.Store == nil {
		return CleanupPreview{}, fmt.Errorf("cleanup store is required")
	}
	if manager.Git == nil {
		return CleanupPreview{}, fmt.Errorf("cleanup git adapter is required")
	}
	root, err := cleanupRoot(manager.WorktreeRoot)
	if err != nil {
		return CleanupPreview{}, err
	}

	leases, err := manager.Store.ListWorktreeLeases(ctx)
	if err != nil {
		return CleanupPreview{}, err
	}
	eligible, err := manager.Store.ListCleanupEligibleWorktreeLeases(ctx, staleBefore)
	if err != nil {
		return CleanupPreview{}, err
	}
	eligibleByID := map[int64]sqlite.WorktreeLease{}
	for _, lease := range eligible {
		eligibleByID[lease.ID] = lease
	}

	preview := CleanupPreview{Leases: make([]CleanupPreviewLease, 0, len(leases))}
	for _, lease := range leases {
		decision := CleanupPreviewLease{
			Lease:  lease,
			Action: CleanupActionSkip,
			Reason: cleanupSkipReason(lease),
		}
		if _, ok := eligibleByID[lease.ID]; ok {
			decision.Action = CleanupActionCleanup
			decision.Reason = cleanupEligibleReason(lease)

			if err := validateCleanupPath(root, lease.WorktreePath); err != nil {
				decision.Action = CleanupActionRefuse
				decision.Reason = "unsafe_path"
				decision.Error = err.Error()
				preview.Leases = append(preview.Leases, decision)
				manager.logCleanupDecision(ctx, decision)
				continue
			}
			dirty, err := manager.Git.WorktreeDirty(ctx, lease.WorktreePath)
			if err != nil {
				decision.Action = CleanupActionRefuse
				decision.Reason = "dirty_check_failed"
				decision.Error = err.Error()
				preview.Leases = append(preview.Leases, decision)
				manager.logCleanupDecision(ctx, decision)
				continue
			}
			decision.Dirty = &dirty
			if dirty {
				decision.Action = CleanupActionRefuse
				decision.Reason = "dirty"
			}
		}
		preview.Leases = append(preview.Leases, decision)
		manager.logCleanupDecision(ctx, decision)
	}
	return preview, nil
}

func (manager Manager) CleanupLeases(ctx context.Context, leases []sqlite.WorktreeLease) (CleanupResult, error) {
	return manager.CleanupLeasesWithOptions(ctx, leases, CleanupOptions{})
}

func (manager Manager) CleanupLeasesWithOptions(ctx context.Context, leases []sqlite.WorktreeLease, options CleanupOptions) (CleanupResult, error) {
	if manager.Store == nil {
		return CleanupResult{}, fmt.Errorf("cleanup store is required")
	}
	if manager.Git == nil {
		return CleanupResult{}, fmt.Errorf("cleanup git adapter is required")
	}
	if err := validateCleanupOptions(options); err != nil {
		return CleanupResult{}, err
	}
	root, err := cleanupRoot(manager.WorktreeRoot)
	if err != nil {
		return CleanupResult{}, err
	}
	return manager.cleanupLeases(ctx, root, leases, options)
}

func (manager Manager) cleanupLeases(ctx context.Context, root string, leases []sqlite.WorktreeLease, options CleanupOptions) (CleanupResult, error) {
	result := CleanupResult{}
	var cleanupErr error
	for _, lease := range leases {
		if err := validateCleanupPath(root, lease.WorktreePath); err != nil {
			manager.logCleanupRejected(ctx, lease, "unsafe_path", err)
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("validate worktree lease %d: %w", lease.ID, err))
			continue
		}
		dirty, err := manager.Git.WorktreeDirty(ctx, lease.WorktreePath)
		if err != nil {
			manager.logCleanupRejected(ctx, lease, "dirty_check_failed", err)
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("check dirty worktree lease %d: %w", lease.ID, err))
			continue
		}
		if dirty && !options.ForceDirty {
			manager.logCleanupRejected(ctx, lease, "dirty", ErrDirtyWorktree)
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("refusing to cleanup dirty worktree lease %d: %w", lease.ID, ErrDirtyWorktree))
			continue
		}
		if err := manager.Git.RemoveWorktree(ctx, lease.RepoRoot, lease.WorktreePath); err != nil && !errors.Is(err, ErrWorktreeAlreadyRemoved) {
			manager.logCleanupRejected(ctx, lease, "remove_failed", err)
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove worktree lease %d: %w", lease.ID, err))
			continue
		}
		updated, err := manager.Store.MarkWorktreeLeaseCleanedUp(ctx, lease.ID)
		if err != nil {
			manager.logCleanupRejected(ctx, lease, "mark_cleaned_failed", err)
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("mark cleaned lease %d: %w", lease.ID, err))
			continue
		}
		manager.logCleanupRemoved(ctx, updated)
		result.Removed = append(result.Removed, updated)
	}

	return result, cleanupErr
}

func validateCleanupOptions(options CleanupOptions) error {
	if !options.ForceDirty {
		return nil
	}
	if strings.TrimSpace(options.ApprovedBy) == "" {
		return fmt.Errorf("force dirty cleanup requires approval identity")
	}
	if strings.TrimSpace(options.ApprovalReason) == "" {
		return fmt.Errorf("force dirty cleanup requires approval reason")
	}
	return nil
}

func (manager Manager) logCleanupDecision(ctx context.Context, decision CleanupPreviewLease) {
	switch decision.Action {
	case CleanupActionCleanup:
		manager.logCleanupRecord(ctx, logs.LevelInfo, "workspace lease cleanup selected", "selected", decision.Reason, decision.Lease, decision.Error)
	case CleanupActionRefuse:
		manager.logCleanupRecord(ctx, logs.LevelWarn, "workspace lease cleanup rejected", "rejected", decision.Reason, decision.Lease, decision.Error)
	default:
		manager.logCleanupRecord(ctx, logs.LevelInfo, "workspace lease cleanup skipped", "skipped", decision.Reason, decision.Lease, decision.Error)
	}
}

func (manager Manager) logCleanupRejected(ctx context.Context, lease sqlite.WorktreeLease, reason string, err error) {
	errorText := ""
	if err != nil {
		errorText = err.Error()
	}
	manager.logCleanupRecord(ctx, logs.LevelWarn, "workspace lease cleanup rejected", "rejected", reason, lease, errorText)
}

func (manager Manager) logCleanupRemoved(ctx context.Context, lease sqlite.WorktreeLease) {
	manager.logCleanupRecord(ctx, logs.LevelInfo, "workspace lease cleanup removed", "removed", "removed", lease, "")
}

func (manager Manager) logCleanupRecord(ctx context.Context, level logs.Level, message string, outcome string, reason string, lease sqlite.WorktreeLease, errorText string) {
	if manager.Logger == nil {
		return
	}
	fields := map[string]any{
		"operation":     "cleanup",
		"outcome":       outcome,
		"lease_id":      lease.ID,
		"branch_name":   lease.BranchName,
		"worktree_path": lease.WorktreePath,
		"repo_root":     lease.RepoRoot,
		"reason":        reason,
	}
	if errorText != "" {
		fields["error"] = errorText
	}
	_ = manager.Logger.Log(logs.Record{
		Level:         level,
		Component:     "workspace",
		Message:       message,
		CorrelationID: fmt.Sprintf("lease:%d", lease.ID),
		Scope:         "project",
		ProjectID:     int64Ptr(lease.ProjectID),
		TaskID:        int64Ptr(lease.TaskID),
		RunID:         int64Ptr(lease.RunID),
		Fields:        fields,
	})
}

func int64Ptr(value int64) *int64 {
	return &value
}

func cleanupEligibleReason(lease sqlite.WorktreeLease) string {
	if lease.State == "active" {
		return "stale"
	}
	return "released"
}

func cleanupSkipReason(lease sqlite.WorktreeLease) string {
	if lease.CleanedUpAt != nil || lease.State == "cleaned" {
		return "already_cleaned"
	}
	if strings.TrimSpace(lease.State) == "" {
		return "unknown_state"
	}
	return lease.State
}

func cleanupRoot(root string) (string, error) {
	root = strings.TrimSpace(expandHome(root))
	if root == "" {
		return "", fmt.Errorf("cleanup worktree root is required")
	}
	cleaned, err := absoluteCleanPath(root)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("cleanup worktree root must be absolute: %q", root)
	}
	if cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("cleanup worktree root cannot be filesystem root")
	}
	return cleaned, nil
}

func validateCleanupPath(root string, path string) error {
	path = strings.TrimSpace(expandHome(path))
	if path == "" {
		return fmt.Errorf("cleanup worktree path is required")
	}
	cleaned, err := absoluteCleanPath(path)
	if err != nil {
		return err
	}
	if err := validatePathWithinRoot(root, cleaned); err != nil {
		return err
	}

	resolvedRoot := resolveExistingPath(root)
	resolvedPath := resolveExistingPath(cleaned)
	return validatePathWithinRoot(resolvedRoot, resolvedPath)
}

func absoluteCleanPath(path string) (string, error) {
	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) {
		return cleaned, nil
	}
	return filepath.Abs(cleaned)
}

func resolveExistingPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return filepath.Clean(resolved)
}

func validatePathWithinRoot(root string, path string) error {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if relative == "." {
		return fmt.Errorf("refusing to cleanup workspace root %q", root)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to cleanup worktree outside workspace root: %q", path)
	}
	return nil
}
