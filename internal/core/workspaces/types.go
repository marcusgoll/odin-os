package workspaces

type WorkspacePolicy string

type Workspace struct {
	ID                  int64
	Key                 string
	Name                string
	OwnerRef            string
	Status              string
	DefaultCompanionKey string
	Policy              WorkspacePolicy
}

const (
	DefaultWorkspaceKey                              = "default"
	DefaultWorkspaceName                             = "Default Workspace"
	DefaultWorkspaceOwnerRef                         = "operator"
	WorkspaceStatusActive                            = "active"
	DefaultWorkspaceCompanionKey                     = "primary"
	DefaultWorkspaceCompanionTitle                   = "Primary Assistant"
	DefaultWorkspaceCompanionKind                    = "assistant"
	DefaultWorkspaceCompanionCharter                 = "Default companion for this workspace."
	DefaultWorkspaceCompanionStatus                  = "active"
	DefaultWorkspacePolicy           WorkspacePolicy = `{}`
)
