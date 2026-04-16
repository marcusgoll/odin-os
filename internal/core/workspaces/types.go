package workspaces

import "time"

const (
	DefaultWorkspaceKey  = "marcus"
	DefaultWorkspaceName = "Marcus Workspace"
	DefaultOwnerRef      = "marcus"

	StatusActive = "active"
)

type Workspace struct {
	ID                  int64
	Key                 string
	Name                string
	OwnerRef            string
	Status              string
	DefaultCompanionKey string
	PolicyJSON          string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
