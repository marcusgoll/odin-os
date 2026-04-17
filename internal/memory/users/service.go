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

func (service Service) Remember(ctx context.Context, memoryType string, summary string, detailsJSON string) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       "global",
		ScopeKey:    "global",
		MemoryType:  memoryType,
		Summary:     summary,
		DetailsJSON: detailsJSON,
	})
}

func (service Service) List(ctx context.Context) ([]sqlite.MemorySummary, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	return service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:    "global",
		ScopeKey: "global",
	})
}
