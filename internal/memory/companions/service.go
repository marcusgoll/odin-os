package companions

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

func (service Service) Recall(ctx context.Context, workspaceKey, companionKey string) ([]sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}

	companionEntries, err := service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		ScopeType: runtimememory.ScopeCompanion,
		ScopeKey:  runtimememory.CompanionScopeKey(workspaceKey, companionKey),
	})
	if err != nil {
		return nil, err
	}
	userEntries, err := users.Service{Store: service.Store}.Recall(ctx, workspaceKey)
	if err != nil {
		return nil, err
	}

	entries := make([]sqlite.MemoryEntry, 0, len(companionEntries)+len(userEntries))
	entries = append(entries, companionEntries...)
	entries = append(entries, userEntries...)
	return entries, nil
}
