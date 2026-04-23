package prompting

import (
	"strings"

	"odin-os/internal/registry"
)

func ComposeSkillPrompt(prompt string, item registry.Item) string {
	return ComposeExecutionPrompt(prompt, registry.Item{}, false, item, true, "")
}

func ComposeExecutionPrompt(prompt string, workflow registry.Item, hasWorkflow bool, skill registry.Item, hasSkill bool, supplementalContext string) string {
	var builder strings.Builder
	if hasWorkflow {
		builder.WriteString("Use the selected Odin workflow while completing this task.\n")
		builder.WriteString("Workflow: ")
		builder.WriteString(workflow.Title)
		builder.WriteString(" (")
		builder.WriteString(workflow.Key)
		builder.WriteString(")\n")
		if summary := strings.TrimSpace(workflow.Summary); summary != "" {
			builder.WriteString("Workflow Summary: ")
			builder.WriteString(summary)
			builder.WriteString("\n")
		}
		if len(workflow.Composes) > 0 {
			builder.WriteString("Workflow Composes: ")
			builder.WriteString(strings.Join(workflow.Composes, ", "))
			builder.WriteString("\n")
		}
		for _, section := range registry.RequiredSections {
			value := strings.TrimSpace(workflow.Sections[section])
			if value == "" {
				continue
			}
			builder.WriteString("\n")
			builder.WriteString("Workflow ")
			builder.WriteString(section)
			builder.WriteString(":\n")
			builder.WriteString(value)
			builder.WriteString("\n")
		}
	}
	if hasSkill {
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString("Use the selected Odin skill while completing this task.\n")
		builder.WriteString("Skill: ")
		builder.WriteString(skill.Title)
		builder.WriteString(" (")
		builder.WriteString(skill.Key)
		builder.WriteString(")\n")
		if summary := strings.TrimSpace(skill.Summary); summary != "" {
			builder.WriteString("Skill Summary: ")
			builder.WriteString(summary)
			builder.WriteString("\n")
		}
		for _, section := range registry.RequiredSections {
			value := strings.TrimSpace(skill.Sections[section])
			if value == "" {
				continue
			}
			builder.WriteString("\n")
			builder.WriteString("Skill ")
			builder.WriteString(section)
			builder.WriteString(":\n")
			builder.WriteString(value)
			builder.WriteString("\n")
		}
	}
	if extra := strings.TrimSpace(supplementalContext); extra != "" {
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(extra)
		builder.WriteString("\n")
	}
	builder.WriteString("\nTask Request:\n")
	builder.WriteString(strings.TrimSpace(prompt))
	builder.WriteString("\n")
	return builder.String()
}
