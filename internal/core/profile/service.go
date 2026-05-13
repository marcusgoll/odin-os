package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	coreworkspaces "odin-os/internal/core/workspaces"
	memoryworkspaces "odin-os/internal/memory/workspaces"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store        *sqlite.Store
	WorkspaceKey string
}

func (service Service) Bootstrap(ctx context.Context) (OperatingProfile, error) {
	return service.Get(ctx)
}

func (service Service) Get(ctx context.Context) (OperatingProfile, error) {
	if service.Store == nil {
		return OperatingProfile{}, fmt.Errorf("profile store is required")
	}

	workspace, err := service.workspace(ctx)
	if err != nil {
		return OperatingProfile{}, err
	}

	record, err := service.Store.GetWorkspaceProfile(ctx, workspace.ID)
	if err == nil {
		return decodeWorkspaceProfile(workspace, record)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return OperatingProfile{}, err
	}

	profile := OperatingProfile{
		WorkspaceID:  workspace.ID,
		WorkspaceKey: workspace.Key,
		Boundaries: Boundaries{
			ApprovalDefaults: ApprovalDefaults{
				RequireHumanApprovalForExternalEffects: true,
			},
		},
	}
	return service.save(ctx, workspace, profile)
}

func (service Service) Update(ctx context.Context, params UpdateParams) (OperatingProfile, error) {
	current, err := service.Get(ctx)
	if err != nil {
		return OperatingProfile{}, err
	}

	if params.QuietHours != nil {
		current.Preferences.QuietHours = *params.QuietHours
	}
	if params.NotificationsEnabled != nil {
		current.Preferences.NotificationsEnabled = *params.NotificationsEnabled
	}
	if params.NotificationBatching != nil {
		current.Preferences.NotificationBatching = *params.NotificationBatching
	}
	if params.RequireHumanApprovalForExternalEffects != nil {
		current.Boundaries.ApprovalDefaults.RequireHumanApprovalForExternalEffects = *params.RequireHumanApprovalForExternalEffects
	}
	if params.ReviewCadence != nil {
		current.CadenceDefaults.ReviewCadence = *params.ReviewCadence
	}

	workspace, err := service.workspace(ctx)
	if err != nil {
		return OperatingProfile{}, err
	}

	updated, err := service.save(ctx, workspace, current)
	if err != nil {
		return OperatingProfile{}, err
	}

	summary, detailsJSON, changed, err := describeProfileUpdate(params)
	if err != nil {
		return OperatingProfile{}, err
	}
	if changed {
		// Profile persistence is the primary mutation; memory summaries are best-effort.
		_, _ = (memoryworkspaces.Service{Store: service.Store}).RememberProfileUpdate(ctx, workspace.ID, summary, detailsJSON)
	}

	return updated, nil
}

func (service Service) save(ctx context.Context, workspace coreworkspaces.Workspace, profile OperatingProfile) (OperatingProfile, error) {
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
		WorkspaceID:         workspace.ID,
		PreferencesJSON:     string(preferencesJSON),
		BoundariesJSON:      string(boundariesJSON),
		CadenceDefaultsJSON: string(cadenceDefaultsJSON),
	})
	if err != nil {
		return OperatingProfile{}, err
	}

	return decodeWorkspaceProfile(workspace, record)
}

func decodeWorkspaceProfile(workspace coreworkspaces.Workspace, record sqlite.WorkspaceProfile) (OperatingProfile, error) {
	var profile OperatingProfile
	profile.WorkspaceID = record.WorkspaceID
	profile.WorkspaceKey = workspace.Key

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

func (service Service) workspace(ctx context.Context) (coreworkspaces.Workspace, error) {
	workspaceService := coreworkspaces.Service{Store: service.Store}
	if service.workspaceKey() == coreworkspaces.DefaultWorkspaceKey {
		return workspaceService.BootstrapDefaultWorkspace(ctx)
	}
	return workspaceService.GetWorkspaceByKey(ctx, service.workspaceKey())
}

func (service Service) workspaceKey() string {
	if service.WorkspaceKey != "" {
		return service.WorkspaceKey
	}
	return DefaultWorkspaceKey
}

func describeProfileUpdate(params UpdateParams) (string, string, bool, error) {
	details := map[string]any{}
	if params.QuietHours != nil {
		details["quiet_hours"] = *params.QuietHours
	}
	if params.NotificationsEnabled != nil {
		details["notifications_enabled"] = *params.NotificationsEnabled
	}
	if params.NotificationBatching != nil {
		details["notification_batching"] = *params.NotificationBatching
	}
	if params.RequireHumanApprovalForExternalEffects != nil {
		details["require_human_approval_for_external_effects"] = *params.RequireHumanApprovalForExternalEffects
	}
	if params.ReviewCadence != nil {
		details["review_cadence"] = *params.ReviewCadence
	}
	if len(details) == 0 {
		return "", "", false, nil
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return "", "", false, err
	}
	return "Updated operating profile", string(payload), true, nil
}
