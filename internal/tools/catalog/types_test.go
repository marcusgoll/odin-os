package catalog

import (
	"testing"

	"odin-os/internal/registry"
)

func TestToolDefinitionCardIsThin(t *testing.T) {
	t.Parallel()

	definition := ToolDefinition{
		Key:        "task_list",
		Title:      "Task List",
		Summary:    "Lists task projections.",
		Scopes:     []string{"project"},
		Tags:       []string{"runtime"},
		CostHint:   CostHintLow,
		BudgetCost: 1,
		SourceRef:  "builtin://task_list",
		Schema: map[string]any{
			"type": "object",
		},
	}

	card := definition.Card()

	if card.Kind != KindTool {
		t.Fatalf("Kind = %q, want %q", card.Kind, KindTool)
	}
	if card.Key != "task_list" {
		t.Fatalf("Key = %q, want task_list", card.Key)
	}
	if card.SourceRef != "builtin://task_list" {
		t.Fatalf("SourceRef = %q, want builtin://task_list", card.SourceRef)
	}
}

func TestCardFromRegistryMapsTypedRegistryCapabilities(t *testing.T) {
	t.Parallel()

	skillCard, ok := CardFromRegistry(registry.Item{
		Kind:    registry.KindSkill,
		Key:     "triage-skill",
		Title:   "Triage Skill",
		Summary: "Classifies requests.",
		Tags:    []string{"intake"},
		AppliesTo: []string{
			"intake",
			"planning",
		},
		Source: registry.SourceInfo{
			RelativePath: "skills/triage-skill.md",
		},
	})
	if !ok {
		t.Fatalf("CardFromRegistry(skill) ok = false, want true")
	}
	if skillCard.Kind != KindSkill {
		t.Fatalf("skill kind = %q, want %q", skillCard.Kind, KindSkill)
	}
	if len(skillCard.AppliesTo) != 2 {
		t.Fatalf("skill applies_to len = %d, want 2", len(skillCard.AppliesTo))
	}

	agentCard, ok := CardFromRegistry(registry.Item{
		Kind:    registry.KindAgent,
		Key:     "triage-agent",
		Title:   "Triage Agent",
		Summary: "Routes work.",
		Scopes:  []string{"global"},
		Tags:    []string{"routing"},
		Source: registry.SourceInfo{
			RelativePath: "agents/triage-agent.md",
		},
	})
	if !ok {
		t.Fatalf("CardFromRegistry(agent) ok = false, want true")
	}
	if agentCard.Kind != KindAgentRole {
		t.Fatalf("agent kind = %q, want %q", agentCard.Kind, KindAgentRole)
	}

	workflowCard, ok := CardFromRegistry(registry.Item{
		Kind:    registry.KindWorkflow,
		Key:     "project-intake",
		Title:   "Project Intake Workflow",
		Summary: "Normalizes project intake.",
		Composes: []string{
			"triage-skill",
			"triage-agent",
		},
		Source: registry.SourceInfo{
			RelativePath: "workflows/project-intake.md",
		},
	})
	if !ok {
		t.Fatalf("CardFromRegistry(workflow) ok = false, want true")
	}
	if workflowCard.Kind != KindWorkflow {
		t.Fatalf("workflow kind = %q, want %q", workflowCard.Kind, KindWorkflow)
	}
	if len(workflowCard.Composes) != 2 {
		t.Fatalf("workflow composes len = %d, want 2", len(workflowCard.Composes))
	}

	commandCard, ok := CardFromRegistry(registry.Item{
		Kind:    registry.KindCommand,
		Key:     "status-command",
		Title:   "Status Command",
		Summary: "Shows current runtime scope.",
		Aliases: []string{"stat"},
		Command: "status",
		Source: registry.SourceInfo{
			RelativePath: "commands/status.md",
		},
	})
	if !ok {
		t.Fatalf("CardFromRegistry(command) ok = false, want true")
	}
	if commandCard.Kind != KindOperatorCommand {
		t.Fatalf("command kind = %q, want %q", commandCard.Kind, KindOperatorCommand)
	}
}
