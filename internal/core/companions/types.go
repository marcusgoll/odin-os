package companions

import "time"

const (
	DefaultOperatorKey = "operator"

	KindOperator = "operator"

	StatusActive = "active"
)

type Companion struct {
	ID                  int64
	WorkspaceID         int64
	Key                 string
	Title               string
	Kind                string
	Charter             string
	Status              string
	InitiativeScopeJSON string
	ToolPolicyJSON      string
	MemoryPolicyJSON    string
	PlanningPolicyJSON  string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
