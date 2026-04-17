package projects

import (
	"context"
	"fmt"

	runtimememory "odin-os/internal/memory"
	"odin-os/internal/memory/users"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store      *sqlite.Store
	ProjectID  int64
	ProjectKey string
}

func (service Service) Recall(ctx context.Context, workspaceKey, projectKey string) ([]sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}

	projectEntries, err := service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		ScopeType: runtimememory.ScopeProject,
		ScopeKey:  runtimememory.ProjectScopeKey(projectKey),
	})
	if err != nil {
		return nil, err
	}
	userEntries, err := users.Service{Store: service.Store}.Recall(ctx, workspaceKey)
	if err != nil {
		return nil, err
	}

	entries := make([]sqlite.MemoryEntry, 0, len(projectEntries)+len(userEntries))
	entries = append(entries, projectEntries...)
	entries = append(entries, userEntries...)
	return entries, nil
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
