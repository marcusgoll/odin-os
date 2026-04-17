package companions

import (
	"context"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

type CreateInput struct {
	WorkspaceID         int64
	Key                 string
	Title               string
	Kind                string
	Charter             string
	Status              string
	InitiativeScopeJSON string
	MemoryPolicyJSON    string
	PlanningPolicyJSON  string
	ToolPolicyJSON      string
}

func (service Service) Create(ctx context.Context, input CreateInput) (sqlite.Companion, error) {
	if service.Store == nil {
		return sqlite.Companion{}, fmt.Errorf("companion store is required")
	}

	return service.Store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         input.WorkspaceID,
		Key:                 input.Key,
		Title:               input.Title,
		Kind:                input.Kind,
		Charter:             input.Charter,
		Status:              input.Status,
		InitiativeScopeJSON: input.InitiativeScopeJSON,
		MemoryPolicyJSON:    input.MemoryPolicyJSON,
		PlanningPolicyJSON:  input.PlanningPolicyJSON,
		ToolPolicyJSON:      input.ToolPolicyJSON,
	})
}

func (service Service) ListByWorkspace(ctx context.Context, workspaceID int64) ([]sqlite.Companion, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("companion store is required")
	}

	return service.Store.ListCompanions(ctx, sqlite.ListCompanionsParams{
		WorkspaceID: workspaceID,
	})
}
