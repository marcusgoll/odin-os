package workspaces

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) BootstrapDefault(ctx context.Context) (Workspace, error) {
	if service.Store == nil {
		return Workspace{}, fmt.Errorf("workspace store is required")
	}

	record, err := service.Store.GetWorkspaceByKey(ctx, DefaultWorkspaceKey)
	if err == nil {
		return fromRecord(record), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, err
	}

	record, err = service.Store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        DefaultWorkspaceKey,
		Name:       DefaultWorkspaceName,
		OwnerRef:   DefaultOwnerRef,
		Status:     StatusActive,
		PolicyJSON: `{}`,
	})
	if err != nil {
		return Workspace{}, err
	}
	return fromRecord(record), nil
}

func (service Service) GetByKey(ctx context.Context, key string) (Workspace, error) {
	if service.Store == nil {
		return Workspace{}, fmt.Errorf("workspace store is required")
	}

	record, err := service.Store.GetWorkspaceByKey(ctx, key)
	if err != nil {
		return Workspace{}, err
	}
	return fromRecord(record), nil
}

func (service Service) ListActive(ctx context.Context) ([]Workspace, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("workspace store is required")
	}

	records, err := service.Store.ListWorkspaces(ctx, sqlite.ListWorkspacesParams{Status: StatusActive})
	if err != nil {
		return nil, err
	}

	workspaces := make([]Workspace, 0, len(records))
	for _, record := range records {
		workspaces = append(workspaces, fromRecord(record))
	}
	return workspaces, nil
}

func fromRecord(record sqlite.Workspace) Workspace {
	return Workspace{
		ID:                  record.ID,
		Key:                 record.Key,
		Name:                record.Name,
		OwnerRef:            record.OwnerRef,
		Status:              record.Status,
		DefaultCompanionKey: record.DefaultCompanionKey,
		PolicyJSON:          record.PolicyJSON,
		CreatedAt:           record.CreatedAt,
		UpdatedAt:           record.UpdatedAt,
	}
}
