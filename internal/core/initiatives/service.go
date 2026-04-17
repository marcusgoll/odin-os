package initiatives

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

type CreateInput struct {
	WorkspaceID      int64
	Key              string
	Title            string
	Kind             string
	Status           string
	Summary          string
	LinkedProjectID  *int64
	OwnerCompanionID *int64
}

func (service Service) Create(ctx context.Context, input CreateInput) (sqlite.Initiative, error) {
	if service.Store == nil {
		return sqlite.Initiative{}, fmt.Errorf("initiative store is required")
	}

	return service.Store.CreateInitiative(ctx, sqlite.CreateInitiativeParams{
		WorkspaceID:      input.WorkspaceID,
		Key:              input.Key,
		Title:            input.Title,
		Kind:             input.Kind,
		Status:           input.Status,
		Summary:          input.Summary,
		LinkedProjectID:  input.LinkedProjectID,
		OwnerCompanionID: input.OwnerCompanionID,
	})
}

func (service Service) ListByWorkspace(ctx context.Context, workspaceID int64) ([]sqlite.Initiative, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("initiative store is required")
	}

	return service.Store.ListInitiatives(ctx, sqlite.ListInitiativesParams{
		WorkspaceID: workspaceID,
	})
}
