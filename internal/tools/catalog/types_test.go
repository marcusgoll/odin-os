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
	if card.CanonicalKey != "task_list" {
		t.Fatalf("CanonicalKey = %q, want task_list", card.CanonicalKey)
	}
	if card.Hidden {
		t.Fatal("Hidden = true, want false")
	}
}

func TestToolDefinitionCardPreservesCanonicalAliasMetadata(t *testing.T) {
	t.Parallel()

	definition := ToolDefinition{
		Key:          "huginn_visual_audit",
		CanonicalKey: "browser_visual_audit",
		Title:        "Browser Visual Audit",
		Summary:      "Captures a live browser snapshot and screenshot for a visual review target.",
		Hidden:       true,
		Scopes:       []string{"global"},
		Tags:         []string{"browser", "visual", "live"},
		CostHint:     CostHintMedium,
		BudgetCost:   2,
		SourceRef:    "builtin://browser_visual_audit",
	}

	card := definition.Card()

	if card.CanonicalKey != "browser_visual_audit" {
		t.Fatalf("CanonicalKey = %q, want browser_visual_audit", card.CanonicalKey)
	}
	if !card.Hidden {
		t.Fatal("Hidden = false, want true")
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
