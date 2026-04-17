package scope

import "odin-os/internal/core/workspaces"

const odinCoreProjectKey = "odin-core"

type Service struct {
	WorkspaceKey string
	CompanionKey string
}

type LegacyScope struct {
	Kind       string
	ProjectKey string
}

func ResolveLegacy(input LegacyScope) ControlScope {
	return Service{}.ResolveLegacy(input)
}

func (service Service) ResolveLegacy(input LegacyScope) ControlScope {
	workspaceKey := service.WorkspaceKey
	if workspaceKey == "" {
		workspaceKey = workspaces.DefaultWorkspaceKey
	}

	companionKey := service.CompanionKey
	if companionKey == "" {
		companionKey = workspaces.DefaultWorkspaceCompanionKey
	}

	switch input.Kind {
	case "global":
		return ControlScope{
			SubjectType:  SubjectTypeWorkspace,
			SubjectKey:   workspaceKey,
			WorkspaceKey: workspaceKey,
			CompanionKey: companionKey,
		}
	case "new-project":
		return ControlScope{
			SubjectType:   SubjectTypeNewProject,
			SubjectKey:    odinCoreProjectKey,
			WorkspaceKey:  workspaceKey,
			InitiativeKey: odinCoreProjectKey,
			ProjectKey:    odinCoreProjectKey,
			CompanionKey:  companionKey,
		}
	case "odin-core":
		return ControlScope{
			SubjectType:   SubjectTypeInitiative,
			SubjectKey:    odinCoreProjectKey,
			WorkspaceKey:  workspaceKey,
			InitiativeKey: odinCoreProjectKey,
			ProjectKey:    odinCoreProjectKey,
			CompanionKey:  companionKey,
		}
	case "project":
		fallthrough
	default:
		projectKey := input.ProjectKey
		if projectKey == "" {
			projectKey = odinCoreProjectKey
		}
		return ControlScope{
			SubjectType:   SubjectTypeInitiative,
			SubjectKey:    projectKey,
			WorkspaceKey:  workspaceKey,
			InitiativeKey: projectKey,
			ProjectKey:    projectKey,
			CompanionKey:  companionKey,
		}
	}
}

func (scope ControlScope) IsGlobal() bool {
	return scope.SubjectType == SubjectTypeWorkspace
}

func (scope ControlScope) TaskScope() string {
	switch scope.SubjectType {
	case SubjectTypeWorkspace:
		return "global"
	case SubjectTypeNewProject:
		return "new-project"
	case SubjectTypeInitiative:
		if scope.ProjectKey == odinCoreProjectKey {
			return "odin-core"
		}
		return "project"
	case SubjectTypeCompanion:
		return "companion"
	default:
		return scope.SubjectType
	}
}

func (scope ControlScope) MatchesTask(projectKey, taskScope string) bool {
	switch scope.SubjectType {
	case SubjectTypeWorkspace:
		return true
	case SubjectTypeNewProject:
		return taskScope == "new-project"
	case SubjectTypeInitiative:
		return projectKey == scope.ProjectKey
	case SubjectTypeCompanion:
		return projectKey == scope.ProjectKey
	default:
		return false
	}
}

func (scope ControlScope) MatchesEvent(eventScope string) bool {
	switch scope.SubjectType {
	case SubjectTypeWorkspace:
		return true
	case SubjectTypeNewProject:
		return eventScope == "new-project"
	case SubjectTypeInitiative:
		if scope.ProjectKey == odinCoreProjectKey {
			return eventScope == "odin-core"
		}
		return eventScope == "project"
	case SubjectTypeCompanion:
		return eventScope == "companion"
	default:
		return false
	}
}
