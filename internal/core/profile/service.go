package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store       *sqlite.Store
	WorkspaceID string
}

func (service Service) Bootstrap(ctx context.Context) (OperatingProfile, error) {
	return service.Get(ctx)
}

func (service Service) Get(ctx context.Context) (OperatingProfile, error) {
	if service.Store == nil {
		return OperatingProfile{}, fmt.Errorf("profile store is required")
	}

	workspaceID := service.workspaceID()
	record, err := service.Store.GetWorkspaceProfile(ctx, workspaceID)
	if err == nil {
		return decodeWorkspaceProfile(record)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return OperatingProfile{}, err
	}

	profile := OperatingProfile{
		WorkspaceID: workspaceID,
		Boundaries: Boundaries{
			ApprovalDefaults: ApprovalDefaults{
				RequireHumanApprovalForExternalEffects: true,
			},
		},
	}
	return service.save(ctx, profile)
}

func (service Service) Update(ctx context.Context, params UpdateParams) (OperatingProfile, error) {
	current, err := service.Get(ctx)
	if err != nil {
		return OperatingProfile{}, err
	}

	if params.QuietHours != nil {
		current.Preferences.QuietHours = *params.QuietHours
	}
	if params.RequireHumanApprovalForExternalEffects != nil {
		current.Boundaries.ApprovalDefaults.RequireHumanApprovalForExternalEffects = *params.RequireHumanApprovalForExternalEffects
	}
	if params.ReviewCadence != nil {
		current.CadenceDefaults.ReviewCadence = *params.ReviewCadence
	}

	return service.save(ctx, current)
}

func (service Service) save(ctx context.Context, profile OperatingProfile) (OperatingProfile, error) {
	if service.Store == nil {
		return OperatingProfile{}, fmt.Errorf("profile store is required")
	}

	preferencesJSON, err := json.Marshal(profile.Preferences)
	if err != nil {
		return OperatingProfile{}, err
	}
	boundariesJSON, err := json.Marshal(profile.Boundaries)
	if err != nil {
		return OperatingProfile{}, err
	}
	cadenceDefaultsJSON, err := json.Marshal(profile.CadenceDefaults)
	if err != nil {
		return OperatingProfile{}, err
	}

	record, err := service.Store.UpsertWorkspaceProfile(ctx, sqlite.UpsertWorkspaceProfileParams{
		WorkspaceID:         service.workspaceID(),
		PreferencesJSON:     string(preferencesJSON),
		BoundariesJSON:      string(boundariesJSON),
		CadenceDefaultsJSON: string(cadenceDefaultsJSON),
	})
	if err != nil {
		return OperatingProfile{}, err
	}

	return decodeWorkspaceProfile(record)
}

func decodeWorkspaceProfile(record sqlite.WorkspaceProfile) (OperatingProfile, error) {
	var profile OperatingProfile
	profile.WorkspaceID = record.WorkspaceID

	if err := json.Unmarshal([]byte(record.PreferencesJSON), &profile.Preferences); err != nil {
		return OperatingProfile{}, err
	}
	if err := json.Unmarshal([]byte(record.BoundariesJSON), &profile.Boundaries); err != nil {
		return OperatingProfile{}, err
	}
	if err := json.Unmarshal([]byte(record.CadenceDefaultsJSON), &profile.CadenceDefaults); err != nil {
		return OperatingProfile{}, err
	}
	profile.CreatedAt = record.CreatedAt
	profile.UpdatedAt = record.UpdatedAt
	return profile, nil
}

func (service Service) workspaceID() string {
	if service.WorkspaceID != "" {
		return service.WorkspaceID
	}
	return DefaultWorkspaceID
}
