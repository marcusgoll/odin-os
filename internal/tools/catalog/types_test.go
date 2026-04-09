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

func TestCardFromRegistryMapsSkillsAndSubAgents(t *testing.T) {
	t.Parallel()

	skillCard, ok := CardFromRegistry(registry.Item{
		Kind:    registry.KindSkill,
		Key:     "triage-skill",
		Title:   "Triage Skill",
		Summary: "Classifies requests.",
		Tags:    []string{"intake"},
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
	if agentCard.Kind != KindSubAgent {
		t.Fatalf("agent kind = %q, want %q", agentCard.Kind, KindSubAgent)
	}
}
