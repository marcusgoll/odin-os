package runs

import (
	"context"
	"fmt"

	memoryroot "odin-os/internal/memory"
	"odin-os/internal/store/sqlite"
)

type Scope struct {
	WorkspaceID  int64
	InitiativeID *int64
	CompanionID  *int64
	TaskID       *int64
	RunID        int64
}

type Service struct {
	Store *sqlite.Store
}

func (service Service) Record(ctx context.Context, scope Scope, input memoryroot.WriteInput) (sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return sqlite.MemoryEntry{}, fmt.Errorf("memory store is required")
	}

	normalized, err := memoryroot.NormalizeWriteInput(input)
	if err != nil {
		return sqlite.MemoryEntry{}, err
	}
	if normalized.VisibilityScope != memoryroot.VisibilityRun {
		return sqlite.MemoryEntry{}, fmt.Errorf("run memory writes require %q visibility", memoryroot.VisibilityRun)
	}

	runID := scope.RunID
	return service.Store.CreateMemoryEntry(ctx, sqlite.CreateMemoryEntryParams{
		WorkspaceID:     scope.WorkspaceID,
		InitiativeID:    scope.InitiativeID,
		CompanionID:     scope.CompanionID,
		TaskID:          scope.TaskID,
		RunID:           &runID,
		EntryType:       string(normalized.EntryType),
		VisibilityScope: string(normalized.VisibilityScope),
		RetentionClass:  string(normalized.RetentionClass),
		Summary:         normalized.Summary,
		Content:         normalized.Content,
		MetadataJSON:    normalized.MetadataJSON,
	})
}

func (service Service) Recall(ctx context.Context, workspaceID int64, runID int64, limit int) ([]sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}

	return service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		WorkspaceID:     workspaceID,
		RunID:           &runID,
		VisibilityScope: string(memoryroot.VisibilityRun),
		Limit:           limit,
	})
}
