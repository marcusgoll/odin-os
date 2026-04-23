package scope

import corescope "odin-os/internal/core/scope"

type Kind string

const (
	ScopeGlobal     Kind = "global"
	ScopeOdinCore   Kind = "odin-core"
	ScopeProject    Kind = "project"
	ScopeNewProject Kind = "new-project"
)

type Target struct {
	ProjectKey    string
	SystemProject bool
}

type ResolveInput struct {
	ExplicitTarget    *Target
	NewProjectFlow    bool
	CWDHintProjectKey string
}

type Resolution struct {
	Kind       Kind
	ProjectKey string
}

func (resolution Resolution) ControlScope() corescope.ControlScope {
	return corescope.ResolveLegacy(corescope.LegacyScope{
		Kind:       string(resolution.Kind),
		ProjectKey: resolution.ProjectKey,
	})
}

func Resolve(input ResolveInput) Resolution {
	if input.ExplicitTarget != nil {
		if input.ExplicitTarget.SystemProject || input.ExplicitTarget.ProjectKey == "odin-core" {
			return Resolution{
				Kind:       ScopeOdinCore,
				ProjectKey: input.ExplicitTarget.ProjectKey,
			}
		}

		return Resolution{
			Kind:       ScopeProject,
			ProjectKey: input.ExplicitTarget.ProjectKey,
		}
	}

	if input.NewProjectFlow {
		return Resolution{Kind: ScopeNewProject}
	}

	return Resolution{Kind: ScopeGlobal}
}
