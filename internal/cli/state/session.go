package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
)

type Mode string

const (
	ModeAsk Mode = "ask"
	ModeAct Mode = "act"
)

type Cache struct {
	ProjectKey          string `json:"project_key,omitempty"`
	Mode                Mode   `json:"mode,omitempty"`
	SelectedSkillKey    string `json:"selected_skill_key,omitempty"`
	SelectedWorkflowKey string `json:"selected_workflow_key,omitempty"`
}

type SessionStore struct {
	Path string
}

type State struct {
	Mode                Mode
	Scope               scope.Resolution
	SelectedSkillKey    string
	SelectedWorkflowKey string
	ActiveTask          string
	ActiveRun           string
}

func (store SessionStore) Load() (Cache, error) {
	content, err := os.ReadFile(store.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return Cache{}, nil
		}
		return Cache{}, err
	}

	var cache Cache
	if err := json.Unmarshal(content, &cache); err != nil {
		return Cache{}, err
	}

	return cache, nil
}

func (store SessionStore) Save(cache Cache) error {
	if err := os.MkdirAll(filepath.Dir(store.Path), 0o755); err != nil {
		return err
	}

	content, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(store.Path, content, 0o644)
}

func ResolveStartupState(cache Cache, registry projects.Registry) State {
	state := State{
		Mode:  ModeAsk,
		Scope: scope.Resolution{Kind: scope.ScopeGlobal},
	}

	if cache.ProjectKey != "" {
		if project, ok := registry.Lookup(cache.ProjectKey); ok {
			state.Scope = scope.Resolve(scope.ResolveInput{
				ExplicitTarget: &scope.Target{
					ProjectKey:    project.Key,
					SystemProject: project.SystemProject,
				},
			})
		}
	}

	state.Mode = SanitizeMode(cache.Mode, state.Scope)
	state.SelectedSkillKey = cache.SelectedSkillKey
	state.SelectedWorkflowKey = cache.SelectedWorkflowKey
	return state
}

func SanitizeMode(mode Mode, resolved scope.Resolution) Mode {
	switch mode {
	case ModeAct:
		if resolved.Kind == scope.ScopeGlobal {
			return ModeAsk
		}
		return ModeAct
	case ModeAsk:
		return ModeAsk
	default:
		return ModeAsk
	}
}
