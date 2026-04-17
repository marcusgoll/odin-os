package initiatives

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

const managedProjectStatusActive = "active"

type Service struct {
	Store *sqlite.Store
}

func (service Service) ReconcileManagedProject(ctx context.Context, workspaceID int64, project sqlite.Project, ownerCompanionID *int64) (Initiative, error) {
	if service.Store == nil {
		return Initiative{}, fmt.Errorf("initiative store is required")
	}

	record, err := service.Store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspaceID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             string(KindManagedProject),
		Status:           managedProjectStatusActive,
		Summary:          "",
		OwnerCompanionID: ownerCompanionID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		return Initiative{}, err
	}

	return toDomainInitiative(record), nil
}

func toDomainInitiative(record sqlite.Initiative) Initiative {
	return Initiative{
		ID:               record.ID,
		WorkspaceID:      record.WorkspaceID,
		Key:              record.Key,
		Title:            record.Title,
		Kind:             Kind(record.Kind),
		Status:           record.Status,
		Summary:          record.Summary,
		OwnerCompanionID: record.OwnerCompanionID,
		LinkedProjectID:  record.LinkedProjectID,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
}
