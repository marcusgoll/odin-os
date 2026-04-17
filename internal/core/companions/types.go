package companions

import "time"

type Kind string

const (
	KindAssistant  Kind = "assistant"
	KindAdvisor    Kind = "advisor"
	KindOperator   Kind = "operator"
	KindSpecialist Kind = "specialist"
)

type Companion struct {
	ID                  int64
	WorkspaceID         int64
	Key                 string
	Title               string
	Kind                Kind
	Charter             string
	Status              string
	InitiativeScopeJSON string
	ToolPolicyJSON      string
	MemoryPolicyJSON    string
	PlanningPolicyJSON  string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
