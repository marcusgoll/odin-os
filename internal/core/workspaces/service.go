package workspaces

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) BootstrapDefaultWorkspace(ctx context.Context) (Workspace, error) {
	if service.Store == nil {
		return Workspace{}, fmt.Errorf("workspace store is required")
	}

	current, err := service.Store.GetWorkspaceByKey(ctx, DefaultWorkspaceKey)
	if err == nil {
		return toDomainWorkspace(current), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, err
	}

	created, err := service.Store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 DefaultWorkspaceKey,
		Name:                DefaultWorkspaceName,
		OwnerRef:            DefaultWorkspaceOwnerRef,
		DefaultCompanionKey: DefaultWorkspaceCompanionKey,
		Status:              WorkspaceStatusActive,
		PolicyJSON:          string(DefaultWorkspacePolicy),
	})
	if err != nil {
		if isWorkspaceKeyConflict(err) {
			current, reloadErr := service.Store.GetWorkspaceByKey(ctx, DefaultWorkspaceKey)
			if reloadErr == nil {
				return toDomainWorkspace(current), nil
			}
			return Workspace{}, reloadErr
		}
		return Workspace{}, err
	}

	return toDomainWorkspace(created), nil
}

func (service Service) GetWorkspaceByKey(ctx context.Context, key string) (Workspace, error) {
	if service.Store == nil {
		return Workspace{}, fmt.Errorf("workspace store is required")
	}

	record, err := service.Store.GetWorkspaceByKey(ctx, key)
	if err != nil {
		return Workspace{}, err
	}
	return toDomainWorkspace(record), nil
}

func (service Service) UpdateWorkspacePolicy(ctx context.Context, key string, policy WorkspacePolicy) (Workspace, error) {
	if service.Store == nil {
		return Workspace{}, fmt.Errorf("workspace store is required")
	}

	current, err := service.Store.GetWorkspaceByKey(ctx, key)
	if err != nil {
		return Workspace{}, err
	}

	updated, err := service.Store.UpdateWorkspacePolicy(ctx, sqlite.UpdateWorkspacePolicyParams{
		WorkspaceID: current.ID,
		PolicyJSON:  string(policy),
	})
	if err != nil {
		return Workspace{}, err
	}

	return toDomainWorkspace(updated), nil
}

func (service Service) ListActiveWorkspaces(ctx context.Context) ([]Workspace, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("workspace store is required")
	}

	records, err := service.Store.ListActiveWorkspaces(ctx)
	if err != nil {
		return nil, err
	}

	workspaces := make([]Workspace, 0, len(records))
	for _, record := range records {
		workspaces = append(workspaces, toDomainWorkspace(record))
	}
	return workspaces, nil
}

func toDomainWorkspace(record sqlite.Workspace) Workspace {
	return Workspace{
		ID:                  record.ID,
		Key:                 record.Key,
		Name:                record.Name,
		OwnerRef:            record.OwnerRef,
		Status:              record.Status,
		DefaultCompanionKey: record.DefaultCompanionKey,
		Policy:              WorkspacePolicy(record.PolicyJSON),
	}
}

func isWorkspaceKeyConflict(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed") && strings.Contains(err.Error(), "workspaces.key")
}
