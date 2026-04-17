package projects

import (
	"context"
	"fmt"

	memoryroot "odin-os/internal/memory"
	memoryworkspaces "odin-os/internal/memory/workspaces"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) Record(ctx context.Context, workspaceID int64, initiativeID int64, input memoryroot.WriteInput) (sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return sqlite.MemoryEntry{}, fmt.Errorf("memory store is required")
	}

	normalized, err := memoryroot.NormalizeWriteInput(input)
	if err != nil {
		return sqlite.MemoryEntry{}, err
	}
	if normalized.VisibilityScope != memoryroot.VisibilityInitiative {
		return sqlite.MemoryEntry{}, fmt.Errorf("project memory writes require %q visibility", memoryroot.VisibilityInitiative)
	}

	return service.Store.CreateMemoryEntry(ctx, sqlite.CreateMemoryEntryParams{
		WorkspaceID:     workspaceID,
		InitiativeID:    &initiativeID,
		EntryType:       string(normalized.EntryType),
		VisibilityScope: string(normalized.VisibilityScope),
		RetentionClass:  string(normalized.RetentionClass),
		Summary:         normalized.Summary,
		Content:         normalized.Content,
		MetadataJSON:    normalized.MetadataJSON,
	})
}

func (service Service) Recall(ctx context.Context, workspaceID int64, initiativeID int64, limit int) ([]sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}

	projectEntries, err := service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		WorkspaceID:     workspaceID,
		InitiativeID:    &initiativeID,
		VisibilityScope: string(memoryroot.VisibilityInitiative),
		Limit:           limit,
	})
	if err != nil {
		return nil, err
	}

	remaining := remainingLimit(limit, len(projectEntries))
	if remaining == 0 {
		return projectEntries, nil
	}

	workspaceEntries, err := (memoryworkspaces.Service{Store: service.Store}).Recall(ctx, workspaceID, remaining)
	if err != nil {
		return nil, err
	}

	return append(projectEntries, workspaceEntries...), nil
}

func remainingLimit(limit int, used int) int {
	if limit <= 0 {
		return 20
	}
	remaining := limit - used
	if remaining <= 0 {
		return 0
	}
	return remaining
}
