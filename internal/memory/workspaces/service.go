package workspaces

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store        *sqlite.Store
	WorkspaceID  int64
	WorkspaceKey string
}

func (service Service) Remember(ctx context.Context, memoryType string, summary string, detailsJSON string) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	if service.WorkspaceID == 0 || service.WorkspaceKey == "" {
		return sqlite.MemorySummary{}, fmt.Errorf("workspace memory requires workspace identity")
	}
	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &service.WorkspaceID,
		Scope:           "workspace",
		ScopeKey:        service.WorkspaceKey,
		VisibilityScope: "workspace",
		RetentionClass:  "durable",
		MemoryType:      memoryType,
		Summary:         summary,
		DetailsJSON:     detailsJSON,
	})
}

func (service Service) List(ctx context.Context) ([]sqlite.MemorySummary, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	if service.WorkspaceID == 0 || service.WorkspaceKey == "" {
		return nil, fmt.Errorf("workspace memory requires workspace identity")
	}
	return service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		WorkspaceID: &service.WorkspaceID,
		Scope:       "workspace",
		ScopeKey:    service.WorkspaceKey,
	})
}
