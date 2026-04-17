package skills

import (
	"strings"

	"gopkg.in/yaml.v3"

	"odin-os/internal/registry"
)

type renderedSkill struct {
	Kind           registry.Kind  `yaml:"kind"`
	Key            string         `yaml:"key"`
	Title          string         `yaml:"title"`
	Summary        string         `yaml:"summary"`
	Status         string         `yaml:"status,omitempty"`
	Version        string         `yaml:"version"`
	Enabled        bool           `yaml:"enabled"`
	Tags           []string       `yaml:"tags,omitempty"`
	Owners         []string       `yaml:"owners,omitempty"`
	Strictness     string         `yaml:"strictness"`
	AppliesTo      []string       `yaml:"applies_to"`
	Scopes         []string       `yaml:"scopes"`
	Permissions    []string       `yaml:"permissions"`
	HandlerType    string         `yaml:"handler_type"`
	HandlerRef     string         `yaml:"handler_ref"`
	TimeoutSeconds int            `yaml:"timeout_seconds"`
	InputSchema    map[string]any `yaml:"input_schema"`
	OutputSchema   map[string]any `yaml:"output_schema"`
}

func Render(spec SkillSpec) (string, error) {
	frontmatter := renderedSkill{
		Kind:           registry.KindSkill,
		Key:            spec.Key,
		Title:          spec.Title,
		Summary:        spec.Summary,
		Status:         spec.Status,
		Version:        spec.Version,
		Enabled:        spec.Enabled,
		Tags:           cloneStrings(spec.Tags),
		Owners:         cloneStrings(spec.Owners),
		Strictness:     spec.Strictness,
		AppliesTo:      cloneStrings(spec.AppliesTo),
		Scopes:         cloneStrings(spec.Scopes),
		Permissions:    cloneStrings(spec.Permissions),
		HandlerType:    spec.HandlerType,
		HandlerRef:     spec.HandlerRef,
		TimeoutSeconds: spec.TimeoutSeconds,
		InputSchema:    cloneAnyMap(spec.InputSchema),
		OutputSchema:   cloneAnyMap(spec.OutputSchema),
	}

	frontmatterBytes, err := yaml.Marshal(frontmatter)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString("---\n")
	builder.Write(frontmatterBytes)
	builder.WriteString("---\n\n")
	builder.WriteString("# ")
	builder.WriteString(strings.TrimSpace(spec.Title))
	builder.WriteString("\n\n")

	for _, section := range registry.RequiredSections {
		builder.WriteString("## ")
		builder.WriteString(section)
		builder.WriteString("\n")
		builder.WriteString(strings.TrimSpace(spec.Sections[section]))
		builder.WriteString("\n\n")
	}

	return builder.String(), nil
}
