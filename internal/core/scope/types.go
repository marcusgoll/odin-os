package scope

type ControlScope struct {
	SubjectType   string
	SubjectKey    string
	WorkspaceKey  string
	InitiativeKey string
	ProjectKey    string
	CompanionKey  string
}

const (
	SubjectTypeWorkspace  = "workspace"
	SubjectTypeInitiative = "initiative"
	SubjectTypeCompanion  = "companion"
	SubjectTypeNewProject = "new-project"
)
