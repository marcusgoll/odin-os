package compiler

import (
	"strings"

	"odin-os/internal/registry"
	"odin-os/internal/registry/validator"
)

func Compile(documents []registry.ParsedDocument, parserDiagnostics []registry.Diagnostic) registry.Snapshot {
	validationDiagnostics := validator.ValidateDocuments(documents)
	diagnostics := append([]registry.Diagnostic{}, parserDiagnostics...)
	diagnostics = append(diagnostics, validationDiagnostics...)
	registry.SortDiagnostics(diagnostics)

	invalidPaths := make(map[string]bool)
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == registry.SeverityError && diagnostic.Path != "" {
			invalidPaths[diagnostic.Path] = true
		}
	}

	snapshot := registry.Snapshot{
		ByKey:       make(map[string]registry.Item),
		ByKind:      make(map[registry.Kind][]registry.Item),
		Diagnostics: diagnostics,
	}

	for _, document := range documents {
		if invalidPaths[document.Source.Path] {
			continue
		}

		item := compileItem(document)

		snapshot.Items = append(snapshot.Items, item)
		snapshot.ByKey[item.Key] = item
		snapshot.ByKind[item.Kind] = append(snapshot.ByKind[item.Kind], item)
	}

	return snapshot
}

func cloneSections(sections map[string]string) map[string]string {
	cloned := make(map[string]string, len(sections))
	for key, value := range sections {
		cloned[key] = value
	}
	return cloned
}

func compileItem(document registry.ParsedDocument) registry.Item {
	frontmatter := document.Frontmatter
	name := strings.TrimSpace(frontmatter.Name)
	if name == "" {
		name = strings.TrimSpace(frontmatter.Key)
	}
	key := strings.TrimSpace(frontmatter.Key)
	if frontmatter.UsesNormalizedManifest() {
		key = name
	} else if key == "" {
		key = name
	}

	availability := frontmatter.Availability
	if strings.TrimSpace(availability.Scope) == "" && len(frontmatter.Scopes) > 0 {
		availability.Scope = frontmatter.Scopes[0]
	}

	permissions := append([]string(nil), frontmatter.Permissions...)
	if len(permissions) == 0 {
		permissions = append([]string(nil), frontmatter.Tools...)
	}

	dependencies := append([]registry.DependencyRef(nil), frontmatter.Dependencies...)
	if len(dependencies) == 0 && len(frontmatter.Composes) > 0 {
		dependencies = make([]registry.DependencyRef, 0, len(frontmatter.Composes))
		for _, compose := range frontmatter.Composes {
			dependencies = append(dependencies, registry.DependencyRef{Name: compose})
		}
	}

	execution := frontmatter.Execution
	if strings.TrimSpace(execution.Mode) == "" {
		switch frontmatter.Kind {
		case registry.KindCommand:
			execution.Mode = "command"
		case registry.KindWorkflow:
			execution.Mode = "workflow"
		case registry.KindSkill:
			execution.Mode = "skill"
		case registry.KindAgent:
			execution.Mode = "agent"
		}
	}

	implementation := frontmatter.Implementation
	if strings.TrimSpace(implementation.Kind) == "" {
		implementation.Kind = "markdown"
	}
	if !frontmatter.UsesNormalizedManifest() && strings.TrimSpace(implementation.Path) == "" {
		implementation.Path = document.Source.RelativePath
	}

	return registry.Item{
		APIVersion:     frontmatter.APIVersion,
		Kind:           frontmatter.Kind,
		Name:           name,
		Version:        frontmatter.Version,
		Availability:   availability,
		Permissions:    permissions,
		InputSchema:    frontmatter.InputSchema,
		OutputSchema:   frontmatter.OutputSchema,
		Dependencies:   dependencies,
		Execution:      execution,
		Implementation: implementation,

		Key:        key,
		Title:      fallbackString(frontmatter.Title, name),
		Summary:    frontmatter.Summary,
		Status:     frontmatter.Status,
		Tags:       append([]string(nil), frontmatter.Tags...),
		Owners:     append([]string(nil), frontmatter.Owners...),
		Role:       frontmatter.Role,
		Scopes:     append([]string(nil), frontmatter.Scopes...),
		Tools:      append([]string(nil), frontmatter.Tools...),
		Strictness: frontmatter.Strictness,
		AppliesTo:  append([]string(nil), frontmatter.AppliesTo...),
		Entrypoint: frontmatter.Entrypoint,
		Composes:   append([]string(nil), frontmatter.Composes...),
		Command:    frontmatter.Command,
		Aliases:    append([]string(nil), frontmatter.Aliases...),
		Sections:   cloneSections(document.Sections),
		Source: registry.SourceInfo{
			Path:         document.Source.Path,
			RelativePath: document.Source.RelativePath,
		},
	}
}

func fallbackString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
