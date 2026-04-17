package projects

import (
	"context"
	"fmt"

	runtimememory "odin-os/internal/memory"
	"odin-os/internal/memory/users"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) Recall(ctx context.Context, workspaceKey, projectKey string) ([]sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}

	projectEntries, err := service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		ScopeType: runtimememory.ScopeProject,
		ScopeKey:  runtimememory.ProjectScopeKey(projectKey),
	})
	if err != nil {
		return nil, err
	}
	userEntries, err := users.Service{Store: service.Store}.Recall(ctx, workspaceKey)
	if err != nil {
		return nil, err
	}

	entries := make([]sqlite.MemoryEntry, 0, len(projectEntries)+len(userEntries))
	entries = append(entries, projectEntries...)
	entries = append(entries, userEntries...)
	return entries, nil
}
