package skills

import (
	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
)

type SkillSpec struct {
	Key            string            `json:"key"`
	Title          string            `json:"title"`
	Summary        string            `json:"summary"`
	Status         string            `json:"status"`
	Version        string            `json:"version"`
	Enabled        bool              `json:"enabled"`
	Tags           []string          `json:"tags"`
	Owners         []string          `json:"owners"`
	Strictness     string            `json:"strictness"`
	AppliesTo      []string          `json:"applies_to"`
	Scopes         []string          `json:"scopes"`
	Permissions    []string          `json:"permissions"`
	HandlerType    string            `json:"handler_type"`
	HandlerRef     string            `json:"handler_ref"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	InputSchema    map[string]any    `json:"input_schema"`
	OutputSchema   map[string]any    `json:"output_schema"`
	Sections       map[string]string `json:"sections"`
}

type Skill struct {
	SkillSpec
	SourcePath     string   `json:"source_path"`
	SourceRef      string   `json:"source_ref"`
	RequiredFields []string `json:"required_fields,omitempty"`
}

type InvocationProject struct {
	ID            int64  `json:"id,omitempty"`
	Key           string `json:"key,omitempty"`
	SystemProject bool   `json:"system_project,omitempty"`
}

type InvocationContext struct {
	ResolvedScopeKind string             `json:"resolved_scope_kind,omitempty"`
	Project           *InvocationProject `json:"project,omitempty"`
	Manifest          projects.Manifest  `json:"manifest,omitempty"`
}

type InvokeRequest struct {
	Key     string            `json:"key"`
	Input   map[string]any    `json:"input"`
	Context InvocationContext `json:"context,omitempty"`
}

type InvokeResponse struct {
	SkillKey    string         `json:"skill_key,omitempty"`
	Status      string         `json:"status"`
	Summary     string         `json:"summary"`
	Output      map[string]any `json:"output,omitempty"`
	Artifacts   []string       `json:"artifacts,omitempty"`
	RawRef      string         `json:"raw_ref,omitempty"`
	RawOutput   string         `json:"raw_output,omitempty"`
	Permissions []string       `json:"permissions,omitempty"`
}

func fromRegistryItem(item registry.Item) Skill {
	return Skill{
		SkillSpec: SkillSpec{
			Key:            item.Key,
			Title:          item.Title,
			Summary:        item.Summary,
			Status:         item.Status,
			Version:        item.Version,
			Enabled:        item.Enabled,
			Tags:           cloneStrings(item.Tags),
			Owners:         cloneStrings(item.Owners),
			Strictness:     item.Strictness,
			AppliesTo:      cloneStrings(item.AppliesTo),
			Scopes:         cloneStrings(item.Scopes),
			Permissions:    cloneStrings(item.Permissions),
			HandlerType:    item.HandlerType,
			HandlerRef:     item.HandlerRef,
			TimeoutSeconds: item.TimeoutSeconds,
			InputSchema:    cloneAnyMap(item.InputSchema),
			OutputSchema:   cloneAnyMap(item.OutputSchema),
			Sections:       cloneSections(item.Sections),
		},
		SourcePath: item.Source.Path,
		SourceRef:  item.Source.RelativePath,
	}
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func cloneSections(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}

	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneAnyValue(value)
	}
	return cloned
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i := range typed {
			cloned[i] = cloneAnyValue(typed[i])
		}
		return cloned
	default:
		return typed
	}
}
