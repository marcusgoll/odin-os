package initiatives

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) Create(ctx context.Context, input CreateInput) (Initiative, error) {
	if service.Store == nil {
		return Initiative{}, fmt.Errorf("initiative store is required")
	}

	record, err := service.Store.CreateInitiative(ctx, sqlite.CreateInitiativeParams{
		WorkspaceID:      input.WorkspaceID,
		Key:              input.Key,
		Title:            input.Title,
		Kind:             input.Kind,
		Status:           input.Status,
		Summary:          input.Summary,
		LinkedProjectID:  input.LinkedProjectID,
		OwnerCompanionID: input.OwnerCompanionID,
	})
	if err != nil {
		return Initiative{}, err
	}
	return fromRecord(record), nil
}

func (service Service) ReconcileManagedProject(ctx context.Context, input ManagedProjectInput) (Initiative, error) {
	if service.Store == nil {
		return Initiative{}, fmt.Errorf("initiative store is required")
	}

	record, err := service.Store.ReconcileManagedProjectInitiative(ctx, sqlite.ReconcileManagedProjectInitiativeParams{
		WorkspaceID: input.WorkspaceID,
		ProjectID:   input.ProjectID,
		Key:         input.ProjectKey,
		Title:       input.ProjectName,
		Status:      input.Status,
		Summary:     input.Summary,
	})
	if err != nil {
		return Initiative{}, err
	}
	return fromRecord(record), nil
}

func (service Service) ListForWorkspace(ctx context.Context, workspaceID int64) ([]Initiative, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("initiative store is required")
	}

	records, err := service.Store.ListInitiatives(ctx, sqlite.ListInitiativesParams{
		WorkspaceID: &workspaceID,
	})
	if err != nil {
		return nil, err
	}

	initiatives := make([]Initiative, 0, len(records))
	for _, record := range records {
		initiatives = append(initiatives, fromRecord(record))
	}
	return initiatives, nil
}

func fromRecord(record sqlite.Initiative) Initiative {
	return Initiative{
		ID:               record.ID,
		WorkspaceID:      record.WorkspaceID,
		Key:              record.Key,
		Title:            record.Title,
		Kind:             record.Kind,
		Status:           record.Status,
		Summary:          record.Summary,
		LinkedProjectID:  record.LinkedProjectID,
		OwnerCompanionID: record.OwnerCompanionID,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
}
