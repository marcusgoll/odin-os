package scope

import "odin-os/internal/core/controlscope"

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

func ToControlScope(resolution Resolution) controlscope.ControlScope {
	switch resolution.Kind {
	case ScopeProject, ScopeOdinCore:
		return controlscope.ControlScope{
			SubjectType: controlscope.SubjectTypeProject,
			SubjectKey:  resolution.ProjectKey,
			ProjectKey:  resolution.ProjectKey,
		}
	case ScopeNewProject:
		return controlscope.ControlScope{
			SubjectType: controlscope.SubjectTypeProject,
			SubjectKey:  string(ScopeNewProject),
		}
	default:
		return controlscope.ControlScope{}
	}
}
