package controlscope

type SubjectType string

const (
	SubjectTypeWorkspace  SubjectType = "workspace"
	SubjectTypeInitiative SubjectType = "initiative"
	SubjectTypeProject    SubjectType = "project"
)

type ControlScope struct {
	SubjectType   SubjectType
	SubjectKey    string
	WorkspaceKey  string
	InitiativeKey string
	ProjectKey    string
	CompanionKey  string
}
