package projects

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
	Store      *sqlite.Store
	ProjectID  int64
	ProjectKey string
}

func (service Service) Remember(ctx context.Context, memoryType string, summary string, detailsJSON string, sourceTranscriptID *int64) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	if service.ProjectID == 0 || service.ProjectKey == "" {
		return sqlite.MemorySummary{}, fmt.Errorf("project memory requires project identity")
	}
	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:          &service.ProjectID,
		SourceTranscriptID: sourceTranscriptID,
		Scope:              "project",
		ScopeKey:           service.ProjectKey,
		MemoryType:         memoryType,
		Summary:            summary,
		DetailsJSON:        detailsJSON,
	})
}

func (service Service) List(ctx context.Context) ([]sqlite.MemorySummary, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	if service.ProjectID == 0 || service.ProjectKey == "" {
		return nil, fmt.Errorf("project memory requires project identity")
	}

	projectEntries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		ProjectID: &service.ProjectID,
		Scope:     "project",
		ScopeKey:  service.ProjectKey,
	})
	if err != nil {
		return nil, err
	}

	globalEntries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:    "global",
		ScopeKey: "global",
	})
	if err != nil {
		return nil, err
	}

	merged := make([]sqlite.MemorySummary, 0, len(projectEntries)+len(globalEntries))
	merged = append(merged, projectEntries...)
	merged = append(merged, globalEntries...)
	return merged, nil
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

func (service Service) RememberFollowUpCompletion(ctx context.Context, workspaceID int64, initiativeID int64, title string, completedAt time.Time) (sqlite.MemorySummary, error) {
	detailsJSON, err := json.Marshal(map[string]string{
		"title":        title,
		"completed_at": completedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return sqlite.MemorySummary{}, err
	}
	return service.rememberFollowUpState(ctx, workspaceID, initiativeID, memoryroot.MemoryTypeFollowUpCompletion, fmt.Sprintf("Completed follow-up: %s", title), string(detailsJSON))
}

func (service Service) RememberFollowUpOverdue(ctx context.Context, workspaceID int64, initiativeID int64, title string, dueAt time.Time) (sqlite.MemorySummary, error) {
	detailsJSON, err := json.Marshal(map[string]string{
		"title":  title,
		"due_at": dueAt.UTC().Format(time.RFC3339Nano),
		"state":  "overdue",
	})
	if err != nil {
		return sqlite.MemorySummary{}, err
	}
	return service.rememberFollowUpState(ctx, workspaceID, initiativeID, memoryroot.MemoryTypeFollowUpOverdue, fmt.Sprintf("Overdue follow-up: %s", title), string(detailsJSON))
}

func (service Service) rememberFollowUpState(ctx context.Context, workspaceID int64, initiativeID int64, memoryType string, summary string, detailsJSON string) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}

	initiative, err := service.scopeInitiative(ctx, workspaceID, initiativeID)
	if err != nil {
		return sqlite.MemorySummary{}, err
	}

	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       "initiative",
		ScopeKey:    initiative.Key,
		MemoryType:  memoryType,
		Summary:     summary,
		DetailsJSON: detailsJSON,
	})
}

func (service Service) scopeInitiative(ctx context.Context, workspaceID int64, initiativeID int64) (sqlite.Initiative, error) {
	if initiativeID <= 0 {
		return sqlite.Initiative{}, fmt.Errorf("initiative ID is required")
	}

	initiative, err := service.Store.GetInitiativeByID(ctx, initiativeID)
	if err != nil {
		return sqlite.Initiative{}, err
	}
	if workspaceID > 0 && initiative.WorkspaceID != workspaceID {
		return sqlite.Initiative{}, fmt.Errorf("initiative %d does not belong to workspace %d", initiativeID, workspaceID)
	}
	return initiative, nil
}
