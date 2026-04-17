package workspaces

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"odin-os/internal/store/sqlite"
)

const defaultWorkspaceKey = "marcus"

type Service struct {
	Store *sqlite.Store
}

func (service Service) BootstrapDefaultWorkspace(ctx context.Context) (sqlite.Workspace, error) {
	if service.Store == nil {
		return sqlite.Workspace{}, fmt.Errorf("workspace store is required")
	}

	workspace, err := service.Store.GetWorkspaceByKey(ctx, defaultWorkspaceKey)
	if err == nil {
		return workspace, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sqlite.Workspace{}, err
	}

	return service.Store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 defaultWorkspaceKey,
		Name:                "Marcus",
		OwnerRef:            "marcus",
		Status:              "active",
		DefaultCompanionKey: "",
		PolicyJSON:          "{}",
	})
}

func (service Service) GetByKey(ctx context.Context, key string) (sqlite.Workspace, error) {
	if service.Store == nil {
		return sqlite.Workspace{}, fmt.Errorf("workspace store is required")
	}
	return service.Store.GetWorkspaceByKey(ctx, key)
}
