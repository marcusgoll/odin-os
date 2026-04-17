package workitems

import (
	"context"
	"fmt"
	"time"

	"odin-os/internal/core/controlscope"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

func (service Service) Create(ctx context.Context, scope controlscope.ControlScope, title string) (WorkItem, error) {
	return service.create(ctx, scope, title, "", "")
}

func (service Service) CreateWithLegacyScope(ctx context.Context, scope controlscope.ControlScope, title string, legacyScope string) (WorkItem, error) {
	return service.create(ctx, scope, title, "", legacyScope)
}

func (service Service) CreateWithLegacyScopeAndAction(ctx context.Context, scope controlscope.ControlScope, title string, actionKey string, legacyScope string) (WorkItem, error) {
	return service.create(ctx, scope, title, actionKey, legacyScope)
}

func (service Service) create(ctx context.Context, scope controlscope.ControlScope, title string, actionKey string, legacyScope string) (WorkItem, error) {
	if service.Store == nil {
		return WorkItem{}, fmt.Errorf("work item store is required")
	}
	if scope.WorkspaceKey == "" {
		return WorkItem{}, fmt.Errorf("workspace key is required")
	}

	workspace, err := service.Store.GetWorkspaceByKey(ctx, scope.WorkspaceKey)
	if err != nil {
		return WorkItem{}, err
	}

	var initiative sqlite.Initiative
	projectKey := scope.ProjectKey
	var initiativeID *int64
	if scope.InitiativeKey != "" {
		initiative, err = service.Store.GetInitiativeByWorkspaceKey(ctx, workspace.ID, scope.InitiativeKey)
		if err != nil {
			return WorkItem{}, err
		}
		initiativeID = &initiative.ID
	}
	if projectKey == "" && initiative.LinkedProjectID != nil {
		project, err := service.Store.GetProject(ctx, *initiative.LinkedProjectID)
		if err != nil {
			return WorkItem{}, err
		}
		projectKey = project.Key
	}
	if projectKey == "" {
		projectKey = "odin-core"
	}
	project, err := service.Store.GetProjectByKey(ctx, projectKey)
	if err != nil {
		return WorkItem{}, err
	}
	if initiativeID != nil && initiative.LinkedProjectID != nil && *initiative.LinkedProjectID != project.ID {
		return WorkItem{}, fmt.Errorf("initiative %q is linked to a different project", initiative.Key)
	}

	var companionID *int64
	if scope.CompanionKey != "" {
		companion, err := service.Store.GetCompanionByWorkspaceKey(ctx, workspace.ID, scope.CompanionKey)
		if err != nil {
			return WorkItem{}, err
		}
		companionID = &companion.ID
	} else if initiativeID != nil {
		companionID = initiative.OwnerCompanionID
	}

	task, err := service.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		WorkspaceID:  workspace.ID,
		InitiativeID: initiativeID,
		CompanionID:  companionID,
		SubjectType:  scope.SubjectType,
		SubjectKey:   scope.SubjectKey,
		Key:          fmt.Sprintf("work-item-%d", time.Now().UnixNano()),
		Title:        title,
		ActionKey:    actionKey,
		Status:       "queued",
		Scope:        legacyTaskScope(legacyScope, project.Scope),
		RequestedBy:  "operator",
	})
	if err != nil {
		return WorkItem{}, err
	}

	return service.Get(ctx, task.ID)
}

func legacyTaskScope(legacyScope string, fallback string) string {
	if legacyScope != "" {
		return legacyScope
	}
	return fallback
}

func (service Service) Get(ctx context.Context, workItemID int64) (WorkItem, error) {
	if service.Store == nil {
		return WorkItem{}, fmt.Errorf("work item store is required")
	}
	record, err := service.Store.GetWorkItem(ctx, workItemID)
	if err != nil {
		return WorkItem{}, err
	}
	return fromRecord(record), nil
}

func (service Service) LinkCompanion(ctx context.Context, workItemID int64, companionKey string) (WorkItem, error) {
	if service.Store == nil {
		return WorkItem{}, fmt.Errorf("work item store is required")
	}
	workItem, err := service.Get(ctx, workItemID)
	if err != nil {
		return WorkItem{}, err
	}
	workspace, err := service.Store.GetWorkspaceByKey(ctx, workItem.WorkspaceKey)
	if err != nil {
		return WorkItem{}, err
	}
	companion, err := service.Store.GetCompanionByWorkspaceKey(ctx, workspace.ID, companionKey)
	if err != nil {
		return WorkItem{}, err
	}
	if _, err := service.Store.UpdateTaskCompanion(ctx, workItemID, &companion.ID); err != nil {
		return WorkItem{}, err
	}
	return service.Get(ctx, workItemID)
}

func (service Service) LinkProject(ctx context.Context, workItemID int64, projectKey string) (WorkItem, error) {
	if service.Store == nil {
		return WorkItem{}, fmt.Errorf("work item store is required")
	}
	project, err := service.Store.GetProjectByKey(ctx, projectKey)
	if err != nil {
		return WorkItem{}, err
	}
	if _, err := service.Store.UpdateTaskProject(ctx, workItemID, project.ID); err != nil {
		return WorkItem{}, err
	}
	return service.Get(ctx, workItemID)
}

func fromRecord(record sqlite.WorkItem) WorkItem {
	return WorkItem{
		ID:            record.ID,
		Scope:         record.Scope,
		WorkspaceKey:  record.WorkspaceKey,
		InitiativeKey: record.InitiativeKey,
		ProjectKey:    record.ProjectKey,
		CompanionKey:  record.CompanionKey,
		Status:        record.Status,
		Title:         record.Title,
		RequestedBy:   record.RequestedBy,
		CurrentRunID:  record.CurrentRunID,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}
}
