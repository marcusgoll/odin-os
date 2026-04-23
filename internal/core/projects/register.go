package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Registry struct {
	projects      []Manifest
	projectsByKey map[string]Manifest
	systemProject *Manifest
	configPath    string
}

func Register(path string) (Registry, []Diagnostic, error) {
	cfg, err := LoadManifestFile(path)
	if err != nil {
		return Registry{}, nil, err
	}

	registry := Registry{
		projects:      make([]Manifest, len(cfg.Projects)),
		projectsByKey: make(map[string]Manifest, len(cfg.Projects)),
		configPath:    path,
	}
	copy(registry.projects, cfg.Projects)

	for _, project := range registry.projects {
		registry.projectsByKey[project.Key] = project
		if project.SystemProject {
			projectCopy := project
			registry.systemProject = &projectCopy
		}
	}

	diagnostics := Validate(cfg)
	if len(diagnostics) != 0 {
		return registry, diagnostics, nil
	}
	return registry, nil, nil
}

func AppendProject(path string, manifest Manifest) (Registry, []Diagnostic, error) {
	if path == "" {
		return Registry{}, nil, fmt.Errorf("project manifest path is required")
	}

	rawConfig, err := loadRawConfig(path)
	if err != nil {
		return Registry{}, nil, err
	}

	manifest.SourcePath = path
	rawConfig.Projects = append(rawConfig.Projects, manifest)

	validated := resolveConfig(path, rawConfig)
	diagnostics := Validate(validated)
	if len(diagnostics) != 0 {
		return Registry{}, diagnostics, nil
	}

	content, err := yaml.Marshal(rawConfig)
	if err != nil {
		return Registry{}, nil, err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return Registry{}, nil, err
	}

	return Register(path)
}

func UpdateProject(path string, key string, mutate func(*Manifest) error) (Registry, []Diagnostic, error) {
	if path == "" {
		return Registry{}, nil, fmt.Errorf("project manifest path is required")
	}
	if strings.TrimSpace(key) == "" {
		return Registry{}, nil, fmt.Errorf("project key is required")
	}
	if mutate == nil {
		return Registry{}, nil, fmt.Errorf("project update function is required")
	}

	rawConfig, err := loadRawConfig(path)
	if err != nil {
		return Registry{}, nil, err
	}

	found := false
	for index := range rawConfig.Projects {
		if rawConfig.Projects[index].Key != key {
			continue
		}

		project := rawConfig.Projects[index]
		project.SourcePath = path
		if err := mutate(&project); err != nil {
			return Registry{}, nil, err
		}
		rawConfig.Projects[index] = project
		found = true
		break
	}
	if !found {
		return Registry{}, nil, fmt.Errorf("unknown project: %s", key)
	}

	validated := resolveConfig(path, rawConfig)
	diagnostics := Validate(validated)
	if len(diagnostics) != 0 {
		return Registry{}, diagnostics, nil
	}

	content, err := yaml.Marshal(rawConfig)
	if err != nil {
		return Registry{}, nil, err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return Registry{}, nil, err
	}

	return Register(path)
}

func (registry Registry) Lookup(key string) (Manifest, bool) {
	project, ok := registry.projectsByKey[key]
	return project, ok
}

func (registry Registry) SystemProject() (Manifest, bool) {
	if registry.systemProject == nil {
		return Manifest{}, false
	}
	return *registry.systemProject, true
}

func (registry Registry) Projects() []Manifest {
	projects := make([]Manifest, len(registry.projects))
	copy(projects, registry.projects)
	return projects
}

func (registry Registry) ConfigPath() string {
	return registry.configPath
}

func loadRawConfig(path string) (Config, error) {
	var cfg Config

	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func resolveConfig(path string, cfg Config) Config {
	baseDir := filepath.Dir(path)
	for index := range cfg.Projects {
		cfg.Projects[index].SourcePath = path
		cfg.Projects[index].GitRoot = resolveGitRoot(baseDir, cfg.Projects[index].GitRoot)
	}
	return cfg
}
