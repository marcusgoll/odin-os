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
