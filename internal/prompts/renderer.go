package prompts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type TemplateData struct {
	WorkItemID          string
	Role                string
	Title               string
	AcceptanceCriteria  []string
	BehaviorChangeNotes string
	Metadata            map[string]string
}

// Renderer turns Odin-owned prompt templates into worker prompts.
type Renderer interface {
	Render(ctx context.Context, templateName string, data TemplateData) (string, error)
}

type FileRenderer struct {
	Root string
}

func (renderer FileRenderer) Render(ctx context.Context, templateName string, data TemplateData) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	path, err := renderer.templatePath(templateName)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	frontmatter, body, err := splitFrontmatter(string(content))
	if err != nil {
		return "", err
	}
	if err := validateTemplate(templateName, frontmatter, body, data); err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(string(content)))
	builder.WriteString("\n\n")
	builder.WriteString("## Rendered Work Item Context\n")
	writeField(&builder, "Work Item", data.WorkItemID)
	writeField(&builder, "Role", data.Role)
	writeField(&builder, "Title", data.Title)
	if strings.TrimSpace(data.BehaviorChangeNotes) != "" {
		writeField(&builder, "Behavior Changes", data.BehaviorChangeNotes)
	}
	if len(data.AcceptanceCriteria) > 0 {
		builder.WriteString("Acceptance Criteria:\n")
		for _, criterion := range data.AcceptanceCriteria {
			builder.WriteString("- ")
			builder.WriteString(strings.TrimSpace(criterion))
			builder.WriteByte('\n')
		}
	}
	if len(data.Metadata) > 0 {
		builder.WriteString("Metadata:\n")
		keys := make([]string, 0, len(data.Metadata))
		for key := range data.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			builder.WriteString("- ")
			builder.WriteString(key)
			builder.WriteByte('=')
			builder.WriteString(data.Metadata[key])
			builder.WriteByte('\n')
		}
	}

	return strings.TrimRight(builder.String(), "\n") + "\n", nil
}

func (renderer FileRenderer) templatePath(templateName string) (string, error) {
	name := strings.TrimSpace(templateName)
	if name == "" {
		return "", fmt.Errorf("template name is required")
	}
	if strings.Contains(name, string(filepath.Separator)) || strings.Contains(name, "/") || strings.Contains(name, "\\") || name == "." || name == ".." {
		return "", fmt.Errorf("invalid template name %q", templateName)
	}
	root := renderer.Root
	if root == "" {
		root = filepath.Join("prompts", "workers")
	}
	return filepath.Join(root, name+".md"), nil
}

func splitFrontmatter(content string) (map[string]string, string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return nil, normalized, nil
	}
	rest := strings.TrimPrefix(normalized, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return nil, "", fmt.Errorf("template frontmatter is not closed")
	}
	rawFrontmatter := rest[:end]
	body := rest[end+len("\n---\n"):]
	fields := map[string]string{}
	for _, line := range strings.Split(rawFrontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, "", fmt.Errorf("invalid frontmatter line %q", line)
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return fields, body, nil
}

func validateTemplate(templateName string, frontmatter map[string]string, body string, data TemplateData) error {
	if frontmatter["requires_acceptance_criteria"] == "true" && len(data.AcceptanceCriteria) == 0 {
		return fmt.Errorf("template %q requires acceptance criteria before dispatch", templateName)
	}
	if frontmatter["prompt_kind"] != "implementation" {
		return nil
	}
	for _, guardrail := range requiredImplementationGuardrails() {
		if !strings.Contains(body, guardrail) {
			return fmt.Errorf("template %q missing implementation guardrail %q", templateName, guardrail)
		}
	}
	return nil
}

func requiredImplementationGuardrails() []string {
	return []string{
		"Explore existing implementation first.",
		"Do not create duplicate modules.",
		"Reuse existing code where safe.",
		"Document behavior changes.",
		"Run Go quality gates.",
		"Return changed files, tests, risks, and follow-up issues.",
	}
}

func writeField(builder *strings.Builder, label string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	builder.WriteString(label)
	builder.WriteString(": ")
	builder.WriteString(value)
	builder.WriteByte('\n')
}

func PromptSizeBytes(prompt string) int {
	return len([]byte(prompt))
}
