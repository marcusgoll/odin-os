package users

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) Remember(ctx context.Context, memoryType string, summary string, detailsJSON string) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       "global",
		ScopeKey:    "global",
		MemoryType:  memoryType,
		Summary:     summary,
		DetailsJSON: detailsJSON,
	})
}

func (service Service) List(ctx context.Context) ([]sqlite.MemorySummary, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	return service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:    "global",
		ScopeKey: "global",
	})
}
