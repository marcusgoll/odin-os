package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

type ContextPackProposalMemoryParams struct {
	ProposalID  int64
	Status      string
	ProjectID   *int64
	TaskID      *int64
	RunID       *int64
	Scope       Scope
	MemoryType  string
	Summary     string
	DetailsJSON string
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

func (service Service) RecordFromContextPackProposal(ctx context.Context, params ContextPackProposalMemoryParams) (sqlite.MemorySummary, error) {
	if service.Store == nil {
		return sqlite.MemorySummary{}, fmt.Errorf("memory store is required")
	}
	if params.ProposalID <= 0 {
		return sqlite.MemorySummary{}, fmt.Errorf("context pack proposal id is required")
	}
	if strings.TrimSpace(params.Status) != "" && !isAcceptedContextPackStatus(params.Status) {
		return sqlite.MemorySummary{}, fmt.Errorf("knowledge memory persistence requires accepted proposal")
	}
	if params.Scope.Value == "" || params.Scope.Key == "" {
		return sqlite.MemorySummary{}, fmt.Errorf("knowledge memory scope is required")
	}
	memoryType := strings.TrimSpace(params.MemoryType)
	if memoryType == "" {
		return sqlite.MemorySummary{}, fmt.Errorf("knowledge memory type is required")
	}
	detailsJSON, err := withSourceContextPackID(params.DetailsJSON, params.ProposalID)
	if err != nil {
		return sqlite.MemorySummary{}, err
	}
	return service.Store.RecordMemorySummaryForAcceptedContextPacket(ctx, params.ProposalID, sqlite.RecordMemorySummaryParams{
		ProjectID:   params.ProjectID,
		TaskID:      params.TaskID,
		RunID:       params.RunID,
		Scope:       params.Scope.Value,
		ScopeKey:    params.Scope.Key,
		MemoryType:  memoryType,
		Summary:     params.Summary,
		DetailsJSON: detailsJSON,
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

func isAcceptedContextPackStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "accepted":
		return true
	default:
		return false
	}
}

func withSourceContextPackID(detailsJSON string, proposalID int64) (string, error) {
	details := map[string]any{}
	if strings.TrimSpace(detailsJSON) != "" {
		if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
			return "", fmt.Errorf("invalid context pack memory details JSON: %w", err)
		}
	}
	details["source_context_pack_id"] = proposalID
	encoded, err := json.Marshal(details)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
