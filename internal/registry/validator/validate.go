package validator

import (
	"fmt"
	"strings"

	"odin-os/internal/registry"
)

func ValidateDocuments(documents []registry.ParsedDocument) []registry.Diagnostic {
	var diagnostics []registry.Diagnostic
	keys := make(map[string][]registry.ParsedDocument)

	for _, document := range documents {
		diagnostics = append(diagnostics, validateDocument(document)...)
		if document.Frontmatter.Key != "" {
			keys[document.Frontmatter.Key] = append(keys[document.Frontmatter.Key], document)
		}
	}

	for key, duplicates := range keys {
		if len(duplicates) < 2 {
			continue
		}

		for _, duplicate := range duplicates {
			diagnostics = append(diagnostics, registry.ErrorDiagnostic(
				duplicate.Source.Path,
				"duplicate_key",
				fmt.Sprintf("registry key %q is declared more than once", key),
			))
		}
	}

	registry.SortDiagnostics(diagnostics)
	return diagnostics
}

func validateDocument(document registry.ParsedDocument) []registry.Diagnostic {
	var diagnostics []registry.Diagnostic

	if document.Source.ExpectedKind == registry.KindUnknown {
		diagnostics = append(diagnostics, registry.ErrorDiagnostic(document.Source.Path, "invalid_path_kind", "registry file must live under agents, skills, workflows, or commands"))
	}

	if !document.Frontmatter.Kind.Valid() {
		diagnostics = append(diagnostics, registry.ErrorDiagnostic(document.Source.Path, "invalid_kind", "frontmatter kind must be one of agent, skill, workflow, or command"))
	} else if document.Source.ExpectedKind != registry.KindUnknown && document.Frontmatter.Kind != document.Source.ExpectedKind {
		diagnostics = append(diagnostics, registry.ErrorDiagnostic(
			document.Source.Path,
			"kind_mismatch",
			fmt.Sprintf("frontmatter kind %q does not match path kind %q", document.Frontmatter.Kind, document.Source.ExpectedKind),
		))
	}

	if document.Frontmatter.HasUnsupportedAPIVersion() {
		diagnostics = append(diagnostics, registry.ErrorDiagnostic(
			document.Source.Path,
			"unsupported_api_version",
			fmt.Sprintf("frontmatter apiVersion %q is not supported; expected %q", document.Frontmatter.APIVersion, registry.NormalizedAPIVersion),
		))
		return diagnostics
	}

	if document.Frontmatter.UsesNormalizedManifest() {
		requireString(document.Source.Path, &diagnostics, "missing_field", "apiVersion", document.Frontmatter.APIVersion)
		requireString(document.Source.Path, &diagnostics, "missing_field", "name", document.Frontmatter.Name)
		requireString(document.Source.Path, &diagnostics, "missing_field", "version", document.Frontmatter.Version)
		requireString(document.Source.Path, &diagnostics, "missing_field", "availability.scope", document.Frontmatter.Availability.Scope)
		requireList(document.Source.Path, &diagnostics, "missing_field", "permissions", document.Frontmatter.Permissions)
		requireString(document.Source.Path, &diagnostics, "missing_field", "execution.mode", document.Frontmatter.Execution.Mode)
		requireString(document.Source.Path, &diagnostics, "missing_field", "implementation.kind", document.Frontmatter.Implementation.Kind)
		requireNormalizedIdentity(document.Source.Path, &diagnostics, document.Frontmatter.Name, document.Frontmatter.Key)
		requireNormalizedDependencies(document.Source.Path, &diagnostics, document.Frontmatter.Dependencies)
		if document.Frontmatter.Kind.IsInvokable() {
			requireSchema(document.Source.Path, &diagnostics, "missing_field", "inputSchema", document.Frontmatter.InputSchema)
			requireSchema(document.Source.Path, &diagnostics, "missing_field", "outputSchema", document.Frontmatter.OutputSchema)
		}
	} else {
		requireString(document.Source.Path, &diagnostics, "missing_field", "key", document.Frontmatter.Key)
		requireString(document.Source.Path, &diagnostics, "missing_field", "title", document.Frontmatter.Title)
		requireString(document.Source.Path, &diagnostics, "missing_field", "summary", document.Frontmatter.Summary)

		switch document.Frontmatter.Kind {
		case registry.KindAgent:
			requireString(document.Source.Path, &diagnostics, "missing_field", "role", document.Frontmatter.Role)
			requireList(document.Source.Path, &diagnostics, "missing_field", "scopes", document.Frontmatter.Scopes)
			requireList(document.Source.Path, &diagnostics, "missing_field", "tools", document.Frontmatter.Tools)
		case registry.KindSkill:
			requireString(document.Source.Path, &diagnostics, "missing_field", "strictness", document.Frontmatter.Strictness)
			requireList(document.Source.Path, &diagnostics, "missing_field", "applies_to", document.Frontmatter.AppliesTo)
		case registry.KindWorkflow:
			requireString(document.Source.Path, &diagnostics, "missing_field", "entrypoint", document.Frontmatter.Entrypoint)
			requireList(document.Source.Path, &diagnostics, "missing_field", "composes", document.Frontmatter.Composes)
		case registry.KindCommand:
			requireString(document.Source.Path, &diagnostics, "missing_field", "command", document.Frontmatter.Command)
			requireList(document.Source.Path, &diagnostics, "missing_field", "scopes", document.Frontmatter.Scopes)
		}
	}

	for _, section := range registry.RequiredSections {
		value, ok := document.Sections[section]
		if !ok || strings.TrimSpace(value) == "" {
			diagnostics = append(diagnostics, registry.ErrorDiagnostic(
				document.Source.Path,
				"missing_section",
				fmt.Sprintf("required section %q is missing or empty", section),
			))
		}
	}

	return diagnostics
}

func requireString(path string, diagnostics *[]registry.Diagnostic, code string, field string, value string) {
	if strings.TrimSpace(value) == "" {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, code, "required frontmatter field "+field+" is missing"))
	}
}

func requireList(path string, diagnostics *[]registry.Diagnostic, code string, field string, values []string) {
	if len(values) == 0 {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, code, "required frontmatter field "+field+" is missing"))
	}
}

func requireSchema(path string, diagnostics *[]registry.Diagnostic, code string, field string, value registry.SchemaRef) {
	if strings.TrimSpace(value.Ref) == "" && strings.TrimSpace(value.Type) == "" {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, code, "required frontmatter field "+field+" is missing"))
	}
}

func requireDependencies(path string, diagnostics *[]registry.Diagnostic, code string, field string, values []registry.DependencyRef) {
	if len(values) == 0 {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, code, "required frontmatter field "+field+" is missing"))
	}
}

func requireNormalizedIdentity(path string, diagnostics *[]registry.Diagnostic, name string, key string) {
	name = strings.TrimSpace(name)
	key = strings.TrimSpace(key)
	if key != "" && key != name {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			path,
			"invalid_identity",
			fmt.Sprintf("normalized frontmatter key %q must match canonical name %q", key, name),
		))
	}
}

func requireNormalizedDependencies(path string, diagnostics *[]registry.Diagnostic, values []registry.DependencyRef) {
	if len(values) == 0 {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, "missing_field", "required frontmatter field dependencies is missing"))
		return
	}

	for index, value := range values {
		if strings.TrimSpace(string(value.Kind)) == "" || strings.TrimSpace(value.Name) == "" || strings.TrimSpace(value.Version) == "" {
			*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
				path,
				"invalid_dependency",
				fmt.Sprintf("normalized dependency %d must include kind, name, and version", index),
			))
		}
	}
}
