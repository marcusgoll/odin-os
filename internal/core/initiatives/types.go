package initiatives

import "time"

const (
	KindManagedProject = "managed_project"

	StatusActive = "active"
)

type Initiative struct {
	ID               int64
	WorkspaceID      int64
	Key              string
	Title            string
	Kind             string
	Status           string
	Summary          string
	LinkedProjectID  *int64
	OwnerCompanionID *int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateInput struct {
	WorkspaceID      int64
	Key              string
	Title            string
	Kind             string
	Status           string
	Summary          string
	LinkedProjectID  *int64
	OwnerCompanionID *int64
}

type ManagedProjectInput struct {
	WorkspaceID int64
	ProjectID   int64
	ProjectKey  string
	ProjectName string
	Status      string
	Summary     string
}
