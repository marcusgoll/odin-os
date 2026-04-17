package initiatives

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store         *sqlite.Store
	WorkspaceID   int64
	InitiativeID  int64
	InitiativeKey string
	ProjectID     *int64
	ProjectKey    string
}

func (service Service) Remember(ctx context.Context, memoryType string, summary string, detailsJSON string, sourceTranscriptID *int64) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	scope, scopeKey, err := service.scope()
	if err != nil {
		return sqlite.MemorySummary{}, err
	}
	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		WorkspaceID:        &service.WorkspaceID,
		InitiativeID:       &service.InitiativeID,
		ProjectID:          service.ProjectID,
		SourceTranscriptID: sourceTranscriptID,
		Scope:              scope,
		ScopeKey:           scopeKey,
		VisibilityScope:    "initiative",
		RetentionClass:     "durable",
		MemoryType:         memoryType,
		Summary:            summary,
		DetailsJSON:        detailsJSON,
	})
}

func (service Service) List(ctx context.Context) ([]sqlite.MemorySummary, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	scope, scopeKey, err := service.scope()
	if err != nil {
		return nil, err
	}
	return service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		WorkspaceID:  &service.WorkspaceID,
		InitiativeID: &service.InitiativeID,
		ProjectID:    service.ProjectID,
		Scope:        scope,
		ScopeKey:     scopeKey,
	})
}

func (service Service) scope() (string, string, error) {
	if service.WorkspaceID == 0 || service.InitiativeID == 0 || service.InitiativeKey == "" {
		return "", "", fmt.Errorf("initiative memory requires workspace and initiative identity")
	}
	if service.ProjectID != nil {
		if service.ProjectKey == "" {
			return "", "", fmt.Errorf("initiative memory with project lineage requires project identity")
		}
		return "project", service.ProjectKey, nil
	}
	return "initiative", service.InitiativeKey, nil
}
