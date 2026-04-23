package initiatives

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"odin-os/internal/store/sqlite"
)

const managedProjectStatusActive = "active"
const (
	statusActive   = "active"
	statusPaused   = "paused"
	statusArchived = "archived"
)

var supportedNonProjectKinds = map[Kind]struct{}{
	KindGoal:          {},
	KindCase:          {},
	KindRoutine:       {},
	KindCampaign:      {},
	KindPersonalAdmin: {},
}

type Service struct {
	Store *sqlite.Store
}

type UpsertInput struct {
	Key              string
	Title            string
	Kind             Kind
	Status           string
	Summary          string
	OwnerCompanionID *int64
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

func (service Service) UpsertNonProject(ctx context.Context, workspaceID int64, input UpsertInput) (Initiative, error) {
	if service.Store == nil {
		return Initiative{}, fmt.Errorf("initiative store is required")
	}
	if err := validateNonProjectKind(input.Kind); err != nil {
		return Initiative{}, err
	}
	if strings.TrimSpace(input.Key) == "" {
		return Initiative{}, fmt.Errorf("initiative key is required")
	}
	if strings.TrimSpace(input.Title) == "" {
		return Initiative{}, fmt.Errorf("initiative title is required")
	}

	status := strings.TrimSpace(strings.ToLower(input.Status))
	if status == "" {
		status = statusActive
	}

	record, err := service.Store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspaceID,
		Key:              strings.TrimSpace(input.Key),
		Title:            strings.TrimSpace(input.Title),
		Kind:             string(input.Kind),
		Status:           status,
		Summary:          strings.TrimSpace(input.Summary),
		OwnerCompanionID: input.OwnerCompanionID,
	})
	if err != nil {
		return Initiative{}, err
	}

	return toDomainInitiative(record), nil
}

func (service Service) ListInitiatives(ctx context.Context, workspaceID int64) ([]Initiative, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("initiative store is required")
	}

	records, err := service.Store.ListInitiativesByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	initiatives := make([]Initiative, 0, len(records))
	for _, record := range records {
		initiatives = append(initiatives, toDomainInitiative(record))
	}
	return initiatives, nil
}

func (service Service) PauseInitiative(ctx context.Context, workspaceID int64, key string) (Initiative, error) {
	return service.setInitiativeStatus(ctx, workspaceID, key, statusPaused)
}

func (service Service) ArchiveInitiative(ctx context.Context, workspaceID int64, key string) (Initiative, error) {
	return service.setInitiativeStatus(ctx, workspaceID, key, statusArchived)
}

func (service Service) setInitiativeStatus(ctx context.Context, workspaceID int64, key string, status string) (Initiative, error) {
	if service.Store == nil {
		return Initiative{}, fmt.Errorf("initiative store is required")
	}
	current, err := service.Store.GetInitiativeByKey(ctx, workspaceID, key)
	if err != nil {
		return Initiative{}, err
	}
	if current.Kind == string(KindManagedProject) {
		return Initiative{}, fmt.Errorf("managed project initiatives are controlled by project reconciliation")
	}

	updated, err := service.Store.UpdateInitiativeStatus(ctx, sqlite.UpdateInitiativeStatusParams{
		InitiativeID: current.ID,
		Status:       status,
	})
	if err != nil {
		return Initiative{}, err
	}
	return toDomainInitiative(updated), nil
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

func validateNonProjectKind(kind Kind) error {
	if kind == KindManagedProject {
		return fmt.Errorf("managed_project initiatives must be reconciled from projects")
	}
	if _, ok := supportedNonProjectKinds[kind]; ok {
		return nil
	}
	if kind == "" {
		return errors.New("initiative kind is required")
	}
	return fmt.Errorf("unsupported initiative kind: %s", kind)
}
