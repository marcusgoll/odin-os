package worktrees

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"odin-os/internal/store/sqlite"
)

type Git interface {
	RemoveWorktree(context.Context, string, string) error
	WorktreeDirty(context.Context, string) (bool, error)
}

type Manager struct {
	Store        *sqlite.Store
	Git          Git
	WorktreeRoot string
}

var ErrWorktreeAlreadyRemoved = errors.New("worktree already removed")
var ErrDirtyWorktree = errors.New("dirty worktree requires explicit force cleanup")

type CleanupResult struct {
	Removed []sqlite.WorktreeLease
}

type CleanupOptions struct {
	ForceDirty     bool
	ApprovedBy     string
	ApprovalReason string
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
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("validate worktree lease %d: %w", lease.ID, err))
			continue
		}
		dirty, err := manager.Git.WorktreeDirty(ctx, lease.WorktreePath)
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("check dirty worktree lease %d: %w", lease.ID, err))
			continue
		}
		if dirty && !options.ForceDirty {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("refusing to cleanup dirty worktree lease %d: %w", lease.ID, ErrDirtyWorktree))
			continue
		}
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
