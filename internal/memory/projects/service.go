package projects

import (
	"context"
	"fmt"

	memoryinitiatives "odin-os/internal/memory/initiatives"
	memoryworkspaces "odin-os/internal/memory/workspaces"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store         *sqlite.Store
	WorkspaceID   int64
	WorkspaceKey  string
	InitiativeID  int64
	InitiativeKey string
	ProjectID     int64
	ProjectKey    string
}

func (service Service) Remember(ctx context.Context, memoryType string, summary string, detailsJSON string, sourceTranscriptID *int64) (sqlite.MemorySummary, error) {
	initiativeService, err := service.initiativeService()
	if err != nil {
		return sqlite.MemorySummary{}, err
	}
	return initiativeService.Remember(ctx, memoryType, summary, detailsJSON, sourceTranscriptID)
}

func (service Service) List(ctx context.Context) ([]sqlite.MemorySummary, error) {
	initiativeService, err := service.initiativeService()
	if err != nil {
		return nil, err
	}
	projectEntries, err := initiativeService.List(ctx)
	if err != nil {
		return nil, err
	}
	workspaceService, err := service.workspaceService()
	if err != nil {
		return nil, err
	}
	workspaceEntries, err := workspaceService.List(ctx)
	if err != nil {
		return nil, err
	}

	merged := make([]sqlite.MemorySummary, 0, len(projectEntries)+len(workspaceEntries))
	merged = append(merged, projectEntries...)
	merged = append(merged, workspaceEntries...)
	return merged, nil
}

func (service Service) initiativeService() (memoryinitiatives.Service, error) {
	if service.Store == nil {
		return memoryinitiatives.Service{}, fmt.Errorf("memory store is required")
	}
	if service.WorkspaceID == 0 || service.InitiativeID == 0 || service.InitiativeKey == "" || service.ProjectID == 0 || service.ProjectKey == "" {
		return memoryinitiatives.Service{}, fmt.Errorf("project memory requires workspace, initiative, and project identity")
	}
	projectID := service.ProjectID
	return memoryinitiatives.Service{
		Store:         service.Store,
		WorkspaceID:   service.WorkspaceID,
		InitiativeID:  service.InitiativeID,
		InitiativeKey: service.InitiativeKey,
		ProjectID:     &projectID,
		ProjectKey:    service.ProjectKey,
	}, nil
}

func (service Service) workspaceService() (memoryworkspaces.Service, error) {
	if service.Store == nil {
		return memoryworkspaces.Service{}, fmt.Errorf("memory store is required")
	}
	return memoryworkspaces.Service{
		Store:        service.Store,
		WorkspaceID:  service.WorkspaceID,
		WorkspaceKey: service.WorkspaceKey,
	}, nil
}
