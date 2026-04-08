package parser

import (
	"strings"

	"gopkg.in/yaml.v3"

	"odin-os/internal/registry"
)

func ParseSource(source registry.SourceFile, content []byte) (registry.ParsedDocument, []registry.Diagnostic) {
	document := registry.ParsedDocument{
		Source:   source,
		Sections: map[string]string{},
	}

	frontmatterText, body, ok := splitFrontmatter(string(content))
	if !ok {
		return document, []registry.Diagnostic{
			registry.ErrorDiagnostic(source.Path, "missing_frontmatter", "registry file must start with YAML frontmatter"),
		}
	}

	var frontmatter registry.Frontmatter
	decoder := yaml.NewDecoder(strings.NewReader(frontmatterText))
	decoder.KnownFields(true)
	if err := decoder.Decode(&frontmatter); err != nil {
		return document, []registry.Diagnostic{
			registry.ErrorDiagnostic(source.Path, "invalid_frontmatter", "frontmatter YAML is invalid: "+err.Error()),
		}
	}

	sections, order, diagnostics := extractSections(source.Path, body)

	document.Frontmatter = frontmatter
	document.Body = body
	document.Sections = sections
	document.SectionOrder = order

	return document, diagnostics
}

func splitFrontmatter(content string) (string, string, bool) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", "", false
	}

	remainder := strings.TrimPrefix(normalized, "---\n")
	end := strings.Index(remainder, "\n---\n")
	if end < 0 {
		return "", "", false
	}

	frontmatter := remainder[:end]
	body := remainder[end+5:]
	return frontmatter, body, true
}

func extractSections(path string, body string) (map[string]string, []string, []registry.Diagnostic) {
	sections := make(map[string]string)
	var order []string
	var diagnostics []registry.Diagnostic

	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	var current string
	var buffer []string

	flush := func() {
		if current == "" {
			return
		}

		if _, exists := sections[current]; exists {
			diagnostics = append(diagnostics, registry.ErrorDiagnostic(path, "duplicate_section", "section "+current+" is declared more than once"))
			current = ""
			buffer = nil
			return
		}

		sections[current] = strings.TrimSpace(strings.Join(buffer, "\n"))
		order = append(order, current)
		current = ""
		buffer = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			current = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}

		if current != "" {
			buffer = append(buffer, line)
		}
	}

	flush()

	return sections, order, diagnostics
}
