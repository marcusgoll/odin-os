package users

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store          *sqlite.Store
	WorkspaceScope string
	WorkspaceKey   string
}

func (service Service) Remember(ctx context.Context, memoryType string, summary string, detailsJSON string) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	scope, scopeKey := service.scope()
	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       scope,
		ScopeKey:    scopeKey,
		MemoryType:  memoryType,
		Summary:     summary,
		DetailsJSON: detailsJSON,
	})
}

func (service Service) List(ctx context.Context) ([]sqlite.MemorySummary, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	scope, scopeKey := service.scope()
	entries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:    scope,
		ScopeKey: scopeKey,
	})
	if err != nil {
		return nil, err
	}
	if scope == "global" && scopeKey == "global" {
		return entries, nil
	}

	globalEntries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:    "global",
		ScopeKey: "global",
	})
	if err != nil {
		return nil, err
	}

	merged := make([]sqlite.MemorySummary, 0, len(entries)+len(globalEntries))
	merged = append(merged, entries...)
	merged = append(merged, globalEntries...)
	return merged, nil
}

func (service Service) scope() (string, string) {
	if service.WorkspaceScope != "" && service.WorkspaceKey != "" {
		return service.WorkspaceScope, service.WorkspaceKey
	}
	return "global", "global"
}
