package projects

type Registry struct {
	projects      []Manifest
	projectsByKey map[string]Manifest
	systemProject *Manifest
	cutover       CutoverConfig
}

func Register(path string) (Registry, []Diagnostic, error) {
	cfg, err := LoadManifestFile(path)
	if err != nil {
		return Registry{}, nil, err
	}

	diagnostics := Validate(cfg)
	if len(diagnostics) != 0 {
		return Registry{}, diagnostics, nil
	}

	registry := Registry{
		projects:      make([]Manifest, len(cfg.Projects)),
		projectsByKey: make(map[string]Manifest, len(cfg.Projects)),
		cutover:       cfg.Cutover,
	}
	copy(registry.projects, cfg.Projects)

	for _, project := range registry.projects {
		registry.projectsByKey[project.Key] = project
		if project.SystemProject {
			projectCopy := project
			registry.systemProject = &projectCopy
		}
	}

	return registry, nil, nil
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

func (registry Registry) CutoverPilotProject(key string) (CutoverPilotProject, bool) {
	return registry.cutover.PilotProject(key)
}
