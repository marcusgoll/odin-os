package memory

import "fmt"

const (
	ScopeWorkspace      = "workspace_memory"
	ScopeInitiative     = "initiative_memory"
	ScopeCompanion      = "companion_memory"
	ScopeProject        = "project_memory"
	ScopeRun            = "run_memory"
	ScopeUserPreference = "user_preference_memory"
)

func WorkspaceScopeKey(workspaceKey string) string {
	return "workspace:" + workspaceKey
}

func InitiativeScopeKey(workspaceKey, initiativeKey string) string {
	return fmt.Sprintf("workspace:%s/initiative:%s", workspaceKey, initiativeKey)
}

func CompanionScopeKey(workspaceKey, companionKey string) string {
	return fmt.Sprintf("workspace:%s/companion:%s", workspaceKey, companionKey)
}

func ProjectScopeKey(projectKey string) string {
	return "project:" + projectKey
}

func RunScopeKey(runID int64) string {
	return fmt.Sprintf("run:%d", runID)
}

func UserPreferenceScopeKey(userKey string) string {
	return "user:" + userKey
}
