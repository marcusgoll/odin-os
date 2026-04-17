package knowledge

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Scope struct {
	WorkspaceID     *int64
	InitiativeID    *int64
	CompanionID     *int64
	ProjectID       *int64
	Value           string
	Key             string
	VisibilityScope string
	RetentionClass  string
}

type Service struct {
	Store *sqlite.Store
}

func (service Service) Record(ctx context.Context, scope Scope, memoryType string, summary string, detailsJSON string, sourceTranscriptID *int64) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	if scope.Value == "" || scope.Key == "" {
		return sqlite.MemorySummary{}, fmt.Errorf("knowledge memory scope is required")
	}
	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		WorkspaceID:        scope.WorkspaceID,
		InitiativeID:       scope.InitiativeID,
		CompanionID:        scope.CompanionID,
		ProjectID:          scope.ProjectID,
		SourceTranscriptID: sourceTranscriptID,
		Scope:              scope.Value,
		ScopeKey:           scope.Key,
		VisibilityScope:    scope.VisibilityScope,
		RetentionClass:     scope.RetentionClass,
		MemoryType:         memoryType,
		Summary:            summary,
		DetailsJSON:        detailsJSON,
	})
}

func (service Service) List(ctx context.Context, scope Scope, memoryType string) ([]sqlite.MemorySummary, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	if scope.Value == "" || scope.Key == "" {
		return nil, fmt.Errorf("knowledge memory scope is required")
	}
	exactEntries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		WorkspaceID:     scope.WorkspaceID,
		InitiativeID:    scope.InitiativeID,
		CompanionID:     scope.CompanionID,
		ProjectID:       scope.ProjectID,
		Scope:           scope.Value,
		ScopeKey:        scope.Key,
		VisibilityScope: scope.VisibilityScope,
		RetentionClass:  scope.RetentionClass,
		MemoryType:      memoryType,
	})
	if err != nil {
		return nil, err
	}
	return exactEntries, nil
}
