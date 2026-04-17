package users

import (
	"context"
	"fmt"

	runtimememory "odin-os/internal/memory"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) Recall(ctx context.Context, workspaceKey string) ([]sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}

	workspace, err := service.Store.GetWorkspaceByKey(ctx, workspaceKey)
	if err != nil {
		return nil, err
	}
	ownerKey := workspace.OwnerRef
	if ownerKey == "" {
		ownerKey = workspace.Key
	}

	workspaceEntries, err := service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		ScopeType: runtimememory.ScopeWorkspace,
		ScopeKey:  runtimememory.WorkspaceScopeKey(workspaceKey),
	})
	if err != nil {
		return nil, err
	}
	preferenceEntries, err := service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		ScopeType: runtimememory.ScopeUserPreference,
		ScopeKey:  runtimememory.UserPreferenceScopeKey(ownerKey),
	})
	if err != nil {
		return nil, err
	}

	entries := make([]sqlite.MemoryEntry, 0, len(workspaceEntries)+len(preferenceEntries))
	entries = append(entries, workspaceEntries...)
	entries = append(entries, preferenceEntries...)
	return entries, nil
}
