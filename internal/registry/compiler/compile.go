package compiler

import (
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

		item := registry.Item{
			Kind:           document.Frontmatter.Kind,
			Key:            document.Frontmatter.Key,
			Title:          document.Frontmatter.Title,
			Summary:        document.Frontmatter.Summary,
			Status:         document.Frontmatter.Status,
			Version:        document.Frontmatter.Version,
			Enabled:        document.Frontmatter.Enabled != nil && *document.Frontmatter.Enabled,
			Tags:           append([]string(nil), document.Frontmatter.Tags...),
			Owners:         append([]string(nil), document.Frontmatter.Owners...),
			Role:           document.Frontmatter.Role,
			Scopes:         append([]string(nil), document.Frontmatter.Scopes...),
			Tools:          append([]string(nil), document.Frontmatter.Tools...),
			Strictness:     document.Frontmatter.Strictness,
			AppliesTo:      append([]string(nil), document.Frontmatter.AppliesTo...),
			Permissions:    append([]string(nil), document.Frontmatter.Permissions...),
			HandlerType:    document.Frontmatter.HandlerType,
			HandlerRef:     document.Frontmatter.HandlerRef,
			TimeoutSeconds: document.Frontmatter.TimeoutSeconds,
			InputSchema:    cloneAnyMap(document.Frontmatter.InputSchema),
			OutputSchema:   cloneAnyMap(document.Frontmatter.OutputSchema),
			Entrypoint:     document.Frontmatter.Entrypoint,
			Composes:       append([]string(nil), document.Frontmatter.Composes...),
			Command:        document.Frontmatter.Command,
			Aliases:        append([]string(nil), document.Frontmatter.Aliases...),
			Sections:       cloneSections(document.Sections),
			Source: registry.SourceInfo{
				Path:         document.Source.Path,
				RelativePath: document.Source.RelativePath,
			},
		}

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
