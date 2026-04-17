package runs

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store        *sqlite.Store
	WorkspaceID  int64
	InitiativeID int64
	ProjectID    int64
	ProjectKey   string
	TaskID       int64
	RunID        int64
}

func (service Service) RecordTranscript(ctx context.Context, mode string, prompt string, response string, toolSummary string, executor string) (sqlite.ConversationTranscript, error) {
	if service.Store == nil {
		return sqlite.ConversationTranscript{}, fmt.Errorf("memory store is required")
	}
	if service.WorkspaceID == 0 || service.InitiativeID == 0 || service.ProjectID == 0 || service.ProjectKey == "" || service.TaskID == 0 || service.RunID == 0 {
		return sqlite.ConversationTranscript{}, fmt.Errorf("run memory requires workspace, initiative, project, task, and run identity")
	}
	return service.Store.RecordConversationTranscript(ctx, sqlite.RecordConversationTranscriptParams{
		ProjectID:    &service.ProjectID,
		WorkspaceID:  &service.WorkspaceID,
		InitiativeID: &service.InitiativeID,
		TaskID:       &service.TaskID,
		RunID:        &service.RunID,
		Scope:        "project",
		ScopeKey:     service.ProjectKey,
		Mode:         mode,
		Prompt:       prompt,
		Response:     response,
		ToolSummary:  toolSummary,
		Executor:     executor,
	})
}

func (service Service) RememberEpisode(ctx context.Context, summary string, detailsJSON string, sourceTranscriptID *int64) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	if service.WorkspaceID == 0 || service.InitiativeID == 0 || service.ProjectID == 0 || service.ProjectKey == "" || service.TaskID == 0 || service.RunID == 0 {
		return sqlite.MemorySummary{}, fmt.Errorf("run memory requires workspace, initiative, project, task, and run identity")
	}
	return service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:          &service.ProjectID,
		WorkspaceID:        &service.WorkspaceID,
		InitiativeID:       &service.InitiativeID,
		SourceTranscriptID: sourceTranscriptID,
		TaskID:             &service.TaskID,
		RunID:              &service.RunID,
		Scope:              "project",
		ScopeKey:           service.ProjectKey,
		VisibilityScope:    "initiative",
		RetentionClass:     "episodic",
		MemoryType:         "episode",
		Summary:            summary,
		DetailsJSON:        detailsJSON,
	})
}

func (service Service) ListEpisodes(ctx context.Context) ([]sqlite.MemorySummary, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	if service.WorkspaceID == 0 || service.InitiativeID == 0 || service.ProjectID == 0 || service.ProjectKey == "" || service.TaskID == 0 || service.RunID == 0 {
		return nil, fmt.Errorf("run memory requires workspace, initiative, project, task, and run identity")
	}
	return service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		ProjectID:    &service.ProjectID,
		WorkspaceID:  &service.WorkspaceID,
		InitiativeID: &service.InitiativeID,
		TaskID:       &service.TaskID,
		RunID:        &service.RunID,
		Scope:        "project",
		ScopeKey:     service.ProjectKey,
		MemoryType:   "episode",
	})
}
