package companions

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) UpsertCompanion(ctx context.Context, companion Companion) (Companion, error) {
	if service.Store == nil {
		return Companion{}, fmt.Errorf("companion store is required")
	}

	record, err := service.Store.UpsertCompanion(ctx, sqlite.UpsertCompanionParams{
		WorkspaceID:         companion.WorkspaceID,
		Key:                 companion.Key,
		Title:               companion.Title,
		Kind:                string(companion.Kind),
		Charter:             companion.Charter,
		Status:              companion.Status,
		InitiativeScopeJSON: companion.InitiativeScopeJSON,
		ToolPolicyJSON:      companion.ToolPolicyJSON,
		MemoryPolicyJSON:    companion.MemoryPolicyJSON,
		PlanningPolicyJSON:  companion.PlanningPolicyJSON,
	})
	if err != nil {
		return Companion{}, err
	}

	return toDomainCompanion(record), nil
}

func (service Service) GetCompanionByKey(ctx context.Context, workspaceID int64, key string) (Companion, error) {
	if service.Store == nil {
		return Companion{}, fmt.Errorf("companion store is required")
	}

	record, err := service.Store.GetCompanionByKey(ctx, workspaceID, key)
	if err != nil {
		return Companion{}, err
	}

	return toDomainCompanion(record), nil
}

func (service Service) ListCompanions(ctx context.Context, workspaceID int64) ([]Companion, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("companion store is required")
	}

	records, err := service.Store.ListCompanionsByWorkspace(ctx, sqlite.ListCompanionsParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}

	companionList := make([]Companion, 0, len(records))
	for _, record := range records {
		companionList = append(companionList, toDomainCompanion(record))
	}
	return companionList, nil
}

func toDomainCompanion(record sqlite.Companion) Companion {
	return Companion{
		ID:                  record.ID,
		WorkspaceID:         record.WorkspaceID,
		Key:                 record.Key,
		Title:               record.Title,
		Kind:                Kind(record.Kind),
		Charter:             record.Charter,
		Status:              record.Status,
		InitiativeScopeJSON: record.InitiativeScopeJSON,
		ToolPolicyJSON:      record.ToolPolicyJSON,
		MemoryPolicyJSON:    record.MemoryPolicyJSON,
		PlanningPolicyJSON:  record.PlanningPolicyJSON,
		CreatedAt:           record.CreatedAt,
		UpdatedAt:           record.UpdatedAt,
	}
}
