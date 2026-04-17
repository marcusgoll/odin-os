package initiatives

import "time"

type Kind string

const (
	KindManagedProject Kind = "managed_project"
	KindGoal           Kind = "goal"
	KindCase           Kind = "case"
	KindRoutine        Kind = "routine"
	KindCampaign       Kind = "campaign"
	KindPersonalAdmin  Kind = "personal_admin"
)

type Initiative struct {
	ID               int64
	WorkspaceID      int64
	Key              string
	Title            string
	Kind             Kind
	Status           string
	Summary          string
	OwnerCompanionID *int64
	LinkedProjectID  *int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
