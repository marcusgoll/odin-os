package workspaces

import (
	"context"
	"database/sql"
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

func (service Service) RememberProfileUpdate(ctx context.Context, workspaceID int64, summary string, detailsJSON string) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}

	workspaceKey, err := service.workspaceKey(ctx, workspaceID)
	if err != nil {
		return sqlite.MemorySummary{}, err
	}

	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       "workspace",
		ScopeKey:    workspaceKey,
		MemoryType:  string(memoryroot.MemoryTypeOperatingProfileUpdate),
		Summary:     summary,
		DetailsJSON: detailsJSON,
	})
}

func (service Service) workspaceKey(ctx context.Context, workspaceID int64) (string, error) {
	if workspaceID <= 0 {
		return "", fmt.Errorf("workspace ID is required")
	}

	row := service.Store.DB().QueryRowContext(ctx, `SELECT key FROM workspaces WHERE id = ?`, workspaceID)
	var key string
	if err := row.Scan(&key); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("workspace %d not found", workspaceID)
		}
		return "", err
	}
	return key, nil
}
