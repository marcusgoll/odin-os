package users

import (
	"context"
	"fmt"

	memoryworkspaces "odin-os/internal/memory/workspaces"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store        *sqlite.Store
	WorkspaceID  int64
	WorkspaceKey string
}

func (service Service) Remember(ctx context.Context, memoryType string, summary string, detailsJSON string) (sqlite.MemorySummary, error) {
	workspaceService, err := service.workspaceService()
	if err != nil {
		return sqlite.MemorySummary{}, err
	}
	return workspaceService.Remember(ctx, memoryType, summary, detailsJSON)
}

func (service Service) List(ctx context.Context) ([]sqlite.MemorySummary, error) {
	workspaceService, err := service.workspaceService()
	if err != nil {
		return nil, err
	}
	return workspaceService.List(ctx)
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
