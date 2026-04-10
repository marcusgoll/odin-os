package projects

import (
	"context"
	"fmt"

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
