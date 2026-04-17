package companions

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store        *sqlite.Store
	WorkspaceID  int64
	CompanionID  int64
	CompanionKey string
}

func (service Service) Remember(ctx context.Context, memoryType string, summary string, detailsJSON string) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	if service.WorkspaceID == 0 || service.CompanionID == 0 || service.CompanionKey == "" {
		return sqlite.MemorySummary{}, fmt.Errorf("companion memory requires workspace and companion identity")
	}
	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &service.WorkspaceID,
		CompanionID:     &service.CompanionID,
		Scope:           "companion",
		ScopeKey:        service.CompanionKey,
		VisibilityScope: "companion",
		RetentionClass:  "working",
		MemoryType:      memoryType,
		Summary:         summary,
		DetailsJSON:     detailsJSON,
	})
}

func (service Service) List(ctx context.Context) ([]sqlite.MemorySummary, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	if service.WorkspaceID == 0 || service.CompanionID == 0 || service.CompanionKey == "" {
		return nil, fmt.Errorf("companion memory requires workspace and companion identity")
	}
	return service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		WorkspaceID: &service.WorkspaceID,
		CompanionID: &service.CompanionID,
		Scope:       "companion",
		ScopeKey:    service.CompanionKey,
	})
}
