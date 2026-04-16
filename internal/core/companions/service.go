package companions

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

const defaultOperatorTitle = "Operator"

const defaultOperatorCharter = "Run the workspace operating rhythm."

type Service struct {
	Store *sqlite.Store
}

func (service Service) BootstrapDefaultOperator(ctx context.Context, workspaceID int64) (Companion, error) {
	if service.Store == nil {
		return Companion{}, fmt.Errorf("companion store is required")
	}

	record, err := service.Store.EnsureCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspaceID,
		Key:                 DefaultOperatorKey,
		Title:               defaultOperatorTitle,
		Kind:                KindOperator,
		Charter:             defaultOperatorCharter,
		Status:              StatusActive,
		InitiativeScopeJSON: `{"mode":"all"}`,
		ToolPolicyJSON:      `{"mode":"deny","allowed":[]}`,
		MemoryPolicyJSON:    `{"retention":"workspace"}`,
		PlanningPolicyJSON:  `{"mode":"stepwise"}`,
	})
	if err != nil {
		return Companion{}, err
	}
	if err := service.Store.SetWorkspaceDefaultCompanion(ctx, workspaceID, record.Key); err != nil {
		return Companion{}, err
	}

	return fromRecord(record), nil
}

func (service Service) ListForWorkspace(ctx context.Context, workspaceID int64) ([]Companion, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("companion store is required")
	}

	records, err := service.Store.ListCompanions(ctx, sqlite.ListCompanionsParams{WorkspaceID: &workspaceID})
	if err != nil {
		return nil, err
	}

	companions := make([]Companion, 0, len(records))
	for _, record := range records {
		companions = append(companions, fromRecord(record))
	}
	return companions, nil
}

func (service Service) AssignToInitiative(ctx context.Context, initiativeID int64, companionID int64) error {
	if service.Store == nil {
		return fmt.Errorf("companion store is required")
	}
	return service.Store.AssignInitiativeCompanion(ctx, initiativeID, companionID)
}

func fromRecord(record sqlite.Companion) Companion {
	return Companion{
		ID:                  record.ID,
		WorkspaceID:         record.WorkspaceID,
		Key:                 record.Key,
		Title:               record.Title,
		Kind:                record.Kind,
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
