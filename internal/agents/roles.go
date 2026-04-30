package agents

type Role string

const (
	RoleTriage   Role = "triage"
	RolePlanner  Role = "planner"
	RoleBuilder  Role = "builder"
	RoleQA       Role = "qa"
	RoleReviewer Role = "reviewer"
	RoleSecurity Role = "security"
)
