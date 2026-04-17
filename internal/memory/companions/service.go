package companions

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	memoryroot "odin-os/internal/memory"
	memoryworkspaces "odin-os/internal/memory/workspaces"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

const (
	MemoryTypeFollowUpCompletion = memoryroot.MemoryTypeFollowUpCompletion
	MemoryTypeFollowUpOverdue    = memoryroot.MemoryTypeFollowUpOverdue
)

func (service Service) Record(ctx context.Context, workspaceID int64, companionID int64, input memoryroot.WriteInput) (sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return sqlite.MemoryEntry{}, fmt.Errorf("memory store is required")
	}

	normalized, err := memoryroot.NormalizeWriteInput(input)
	if err != nil {
		return sqlite.MemoryEntry{}, err
	}
	if normalized.VisibilityScope != memoryroot.VisibilityCompanion {
		return sqlite.MemoryEntry{}, fmt.Errorf("companion memory writes require %q visibility", memoryroot.VisibilityCompanion)
	}

	return service.Store.CreateMemoryEntry(ctx, sqlite.CreateMemoryEntryParams{
		WorkspaceID:     workspaceID,
		CompanionID:     &companionID,
		EntryType:       string(normalized.EntryType),
		VisibilityScope: string(normalized.VisibilityScope),
		RetentionClass:  string(normalized.RetentionClass),
		Summary:         normalized.Summary,
		Content:         normalized.Content,
		MetadataJSON:    normalized.MetadataJSON,
	})
}

func (service Service) Recall(ctx context.Context, workspaceID int64, companionID int64, limit int) ([]sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}

	companionEntries, err := service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		WorkspaceID:     workspaceID,
		CompanionID:     &companionID,
		VisibilityScope: string(memoryroot.VisibilityCompanion),
		Limit:           limit,
	})
	if err != nil {
		return nil, err
	}

	remaining := remainingLimit(limit, len(companionEntries))
	if remaining == 0 {
		return companionEntries, nil
	}

	workspaceEntries, err := (memoryworkspaces.Service{Store: service.Store}).Recall(ctx, workspaceID, remaining)
	if err != nil {
		return nil, err
	}

	return append(companionEntries, workspaceEntries...), nil
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

func (service Service) RememberFollowUpCompletion(ctx context.Context, workspaceID int64, companionID int64, title string, completedAt time.Time) (sqlite.MemorySummary, error) {
	detailsJSON, err := json.Marshal(map[string]string{
		"title":        title,
		"completed_at": completedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return sqlite.MemorySummary{}, err
	}
	return service.rememberFollowUpState(ctx, workspaceID, companionID, MemoryTypeFollowUpCompletion, fmt.Sprintf("Completed follow-up: %s", title), string(detailsJSON))
}

func (service Service) RememberFollowUpOverdue(ctx context.Context, workspaceID int64, companionID int64, title string, dueAt time.Time) (sqlite.MemorySummary, error) {
	detailsJSON, err := json.Marshal(map[string]string{
		"title":  title,
		"due_at": dueAt.UTC().Format(time.RFC3339Nano),
		"state":  "overdue",
	})
	if err != nil {
		return sqlite.MemorySummary{}, err
	}
	return service.rememberFollowUpState(ctx, workspaceID, companionID, MemoryTypeFollowUpOverdue, fmt.Sprintf("Overdue follow-up: %s", title), string(detailsJSON))
}

func (service Service) rememberFollowUpState(ctx context.Context, workspaceID int64, companionID int64, memoryType string, summary string, detailsJSON string) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}

	companion, err := service.scopeCompanion(ctx, workspaceID, companionID)
	if err != nil {
		return sqlite.MemorySummary{}, err
	}

	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       "companion",
		ScopeKey:    companion.Key,
		MemoryType:  memoryType,
		Summary:     summary,
		DetailsJSON: detailsJSON,
	})
}

func (service Service) scopeCompanion(ctx context.Context, workspaceID int64, companionID int64) (sqlite.Companion, error) {
	if companionID <= 0 {
		return sqlite.Companion{}, fmt.Errorf("companion ID is required")
	}

	companion, err := service.Store.GetCompanionByID(ctx, companionID)
	if err != nil {
		return sqlite.Companion{}, err
	}
	if workspaceID > 0 && companion.WorkspaceID != workspaceID {
		return sqlite.Companion{}, fmt.Errorf("companion %d does not belong to workspace %d", companionID, workspaceID)
	}
	return companion, nil
}
