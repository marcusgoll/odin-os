package initiatives

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

func (service Service) Recall(ctx context.Context, workspaceKey, initiativeKey string) ([]sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}

	initiativeEntries, err := service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		ScopeType: runtimememory.ScopeInitiative,
		ScopeKey:  runtimememory.InitiativeScopeKey(workspaceKey, initiativeKey),
	})
	if err != nil {
		return nil, err
	}
	userEntries, err := users.Service{Store: service.Store}.Recall(ctx, workspaceKey)
	if err != nil {
		return nil, err
	}

	entries := make([]sqlite.MemoryEntry, 0, len(initiativeEntries)+len(userEntries))
	entries = append(entries, initiativeEntries...)
	entries = append(entries, userEntries...)
	return entries, nil
}
