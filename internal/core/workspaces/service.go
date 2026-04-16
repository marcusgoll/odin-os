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
		if err := service.ensureDefaultWorkspacePolicy(ctx, current.ID); err != nil {
			return Workspace{}, err
		}
		if err := service.ensureDefaultCompanion(ctx, current.ID, current.DefaultCompanionKey); err != nil {
			return Workspace{}, err
		}
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
				if repairErr := service.ensureDefaultWorkspacePolicy(ctx, current.ID); repairErr != nil {
					return Workspace{}, repairErr
				}
				if repairErr := service.ensureDefaultCompanion(ctx, current.ID, current.DefaultCompanionKey); repairErr != nil {
					return Workspace{}, repairErr
				}
				return toDomainWorkspace(current), nil
			}
			return Workspace{}, reloadErr
		}
		return Workspace{}, err
	}

	if err := service.ensureDefaultCompanion(ctx, created.ID, created.DefaultCompanionKey); err != nil {
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

func (service Service) ensureDefaultWorkspacePolicy(ctx context.Context, workspaceID int64) error {
	var policyCount int64
	if err := service.Store.DB().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM workspace_policies
		WHERE workspace_id = ?
	`, workspaceID).Scan(&policyCount); err != nil {
		return err
	}
	if policyCount > 0 {
		return nil
	}

	_, err := service.Store.UpdateWorkspacePolicy(ctx, sqlite.UpdateWorkspacePolicyParams{
		WorkspaceID: workspaceID,
		PolicyJSON:  string(DefaultWorkspacePolicy),
	})
	return err
}

func (service Service) ensureDefaultCompanion(ctx context.Context, workspaceID int64, key string) error {
	_, err := service.Store.GetCompanionByKey(ctx, workspaceID, key)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	_, err = service.Store.UpsertCompanion(ctx, sqlite.UpsertCompanionParams{
		WorkspaceID:         workspaceID,
		Key:                 key,
		Title:               DefaultWorkspaceCompanionTitle,
		Kind:                DefaultWorkspaceCompanionKind,
		Charter:             DefaultWorkspaceCompanionCharter,
		Status:              DefaultWorkspaceCompanionStatus,
		InitiativeScopeJSON: `{}`,
		ToolPolicyJSON:      `{}`,
		MemoryPolicyJSON:    `{}`,
		PlanningPolicyJSON:  `{}`,
	})
	return err
}
