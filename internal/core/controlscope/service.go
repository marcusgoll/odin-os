package controlscope

type Service struct{}

func (Service) ResolveWorkspace(workspaceKey string) ControlScope {
	return ControlScope{
		SubjectType:  SubjectTypeWorkspace,
		SubjectKey:   workspaceKey,
		WorkspaceKey: workspaceKey,
	}
}

func (Service) ResolveInitiative(workspaceKey, initiativeKey string) ControlScope {
	return ControlScope{
		SubjectType:   SubjectTypeInitiative,
		SubjectKey:    initiativeKey,
		WorkspaceKey:  workspaceKey,
		InitiativeKey: initiativeKey,
	}
}

func (Service) ResolveManagedProject(workspaceKey, initiativeKey, projectKey, companionKey string) ControlScope {
	return ControlScope{
		SubjectType:   SubjectTypeProject,
		SubjectKey:    projectKey,
		WorkspaceKey:  workspaceKey,
		InitiativeKey: initiativeKey,
		ProjectKey:    projectKey,
		CompanionKey:  companionKey,
	}
}
