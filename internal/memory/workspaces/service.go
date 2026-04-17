package workspaces

import (
	"context"
	"fmt"

	memoryroot "odin-os/internal/memory"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) Record(ctx context.Context, workspaceID int64, input memoryroot.WriteInput) (sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return sqlite.MemoryEntry{}, fmt.Errorf("memory store is required")
	}

	normalized, err := memoryroot.NormalizeWriteInput(input)
	if err != nil {
		return sqlite.MemoryEntry{}, err
	}
	if normalized.VisibilityScope != memoryroot.VisibilityWorkspace {
		return sqlite.MemoryEntry{}, fmt.Errorf("workspace memory writes require %q visibility", memoryroot.VisibilityWorkspace)
	}

	return service.Store.CreateMemoryEntry(ctx, sqlite.CreateMemoryEntryParams{
		WorkspaceID:     workspaceID,
		EntryType:       string(normalized.EntryType),
		VisibilityScope: string(normalized.VisibilityScope),
		RetentionClass:  string(normalized.RetentionClass),
		Summary:         normalized.Summary,
		Content:         normalized.Content,
		MetadataJSON:    normalized.MetadataJSON,
	})
}

func (service Service) Recall(ctx context.Context, workspaceID int64, limit int) ([]sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}

	return service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		WorkspaceID:     workspaceID,
		VisibilityScope: string(memoryroot.VisibilityWorkspace),
		Limit:           limit,
	})
}
