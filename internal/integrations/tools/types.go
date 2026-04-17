package tools

type ToolRequest struct {
	ToolKey       string
	Scope         string
	WorkspaceKey  string
	InitiativeKey string
	CompanionKey  string
	Parameters    map[string]string
}

type ArtifactReference struct {
	Kind string
	Ref  string
}

type AuthorizationResult struct {
	Allowed bool
	Reason  string
}

type ToolResult struct {
	ToolKey         string
	Summary         string
	Artifacts       []ArtifactReference
	KeyFacts        map[string]string
	FollowOnOptions []string
	RawRef          string
	RawOutput       string
}
