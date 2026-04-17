package runs

import (
	"context"
	"fmt"

	memoryroot "odin-os/internal/memory"
	"odin-os/internal/store/sqlite"
)

type Scope struct {
	WorkspaceID  int64
	InitiativeID *int64
	CompanionID  *int64
	TaskID       *int64
	RunID        int64
}

type Service struct {
	Store      *sqlite.Store
	ProjectID  int64
	ProjectKey string
	TaskID     int64
	RunID      int64
}

func (service Service) RecordTranscript(ctx context.Context, mode string, prompt string, response string, toolSummary string, executor string) (sqlite.ConversationTranscript, error) {
	if service.Store == nil {
		return sqlite.ConversationTranscript{}, fmt.Errorf("memory store is required")
	}
	if service.ProjectID == 0 || service.ProjectKey == "" || service.TaskID == 0 || service.RunID == 0 {
		return sqlite.ConversationTranscript{}, fmt.Errorf("run memory requires project, task, and run identity")
	}
	return service.Store.RecordConversationTranscript(ctx, sqlite.RecordConversationTranscriptParams{
		ProjectID:   &service.ProjectID,
		TaskID:      &service.TaskID,
		RunID:       &service.RunID,
		Scope:       "project",
		ScopeKey:    service.ProjectKey,
		Mode:        mode,
		Prompt:      prompt,
		Response:    response,
		ToolSummary: toolSummary,
		Executor:    executor,
	})
}

func (service Service) RememberEpisode(ctx context.Context, summary string, detailsJSON string, sourceTranscriptID *int64) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	if service.ProjectID == 0 || service.ProjectKey == "" || service.TaskID == 0 || service.RunID == 0 {
		return sqlite.MemorySummary{}, fmt.Errorf("run memory requires project, task, and run identity")
	}
	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:          &service.ProjectID,
		SourceTranscriptID: sourceTranscriptID,
		TaskID:             &service.TaskID,
		RunID:              &service.RunID,
		Scope:              "project",
		ScopeKey:           service.ProjectKey,
		MemoryType:         "episode",
		Summary:            summary,
		DetailsJSON:        detailsJSON,
	})
}

func (service Service) ListEpisodes(ctx context.Context) ([]sqlite.MemorySummary, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	if service.ProjectID == 0 || service.ProjectKey == "" || service.TaskID == 0 || service.RunID == 0 {
		return nil, fmt.Errorf("run memory requires project, task, and run identity")
	}
	return service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		ProjectID:  &service.ProjectID,
		TaskID:     &service.TaskID,
		RunID:      &service.RunID,
		Scope:      "project",
		ScopeKey:   service.ProjectKey,
		MemoryType: "episode",
	})
}

func (service Service) Record(ctx context.Context, scope Scope, input memoryroot.WriteInput) (sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return sqlite.MemoryEntry{}, fmt.Errorf("memory store is required")
	}

	normalized, err := memoryroot.NormalizeWriteInput(input)
	if err != nil {
		return sqlite.MemoryEntry{}, err
	}
	if normalized.VisibilityScope != memoryroot.VisibilityRun {
		return sqlite.MemoryEntry{}, fmt.Errorf("run memory writes require %q visibility", memoryroot.VisibilityRun)
	}

	runID := scope.RunID
	return service.Store.CreateMemoryEntry(ctx, sqlite.CreateMemoryEntryParams{
		WorkspaceID:     scope.WorkspaceID,
		InitiativeID:    scope.InitiativeID,
		CompanionID:     scope.CompanionID,
		TaskID:          scope.TaskID,
		RunID:           &runID,
		EntryType:       string(normalized.EntryType),
		VisibilityScope: string(normalized.VisibilityScope),
		RetentionClass:  string(normalized.RetentionClass),
		Summary:         normalized.Summary,
		Content:         normalized.Content,
		MetadataJSON:    normalized.MetadataJSON,
	})
}

func (service Service) Recall(ctx context.Context, workspaceID int64, runID int64, limit int) ([]sqlite.MemoryEntry, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}

	return service.Store.ListMemoryEntries(ctx, sqlite.ListMemoryEntriesParams{
		WorkspaceID:     workspaceID,
		RunID:           &runID,
		VisibilityScope: string(memoryroot.VisibilityRun),
		Limit:           limit,
	})
}
