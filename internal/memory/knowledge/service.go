package knowledge

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Scope struct {
	ProjectID *int64
	Value     string
	Key       string
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
		ProjectID:          scope.ProjectID,
		SourceTranscriptID: sourceTranscriptID,
		Scope:              scope.Value,
		ScopeKey:           scope.Key,
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
		ProjectID:  scope.ProjectID,
		Scope:      scope.Value,
		ScopeKey:   scope.Key,
		MemoryType: memoryType,
	})
	if err != nil {
		return nil, err
	}
	if scope.ProjectID == nil || scope.Value == "global" {
		return exactEntries, nil
	}

	globalEntries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "global",
		ScopeKey:   "global",
		MemoryType: memoryType,
	})
	if err != nil {
		return nil, err
	}

	merged := make([]sqlite.MemorySummary, 0, len(exactEntries)+len(globalEntries))
	merged = append(merged, exactEntries...)
	merged = append(merged, globalEntries...)
	return merged, nil
}
