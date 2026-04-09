package planner

import (
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/tools/broker"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
)

func TestPrepareStartsFromThinCatalogOnly(t *testing.T) {
	t.Parallel()

	service := Service{
		Broker: broker.New(
			testSnapshot(),
			catalog.BuiltinDefinitions(),
			budgets.Limits{
				Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
				Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
			},
		),
	}

	context, err := service.Prepare("project")
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(context.Cards) == 0 {
		t.Fatalf("Prepare() cards len = 0, want > 0")
	}
	if service.Broker.Usage().ExpandedDefinitions != 0 {
		t.Fatalf("expanded definitions = %d, want 0", service.Broker.Usage().ExpandedDefinitions)
	}
}

func TestMaterializeExpandsOnlySelectedCapability(t *testing.T) {
	t.Parallel()

	service := Service{
		Broker: broker.New(
			testSnapshot(),
			catalog.BuiltinDefinitions(),
			budgets.Limits{
				Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
				Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
			},
		),
	}

	execution, err := service.Materialize("project", []Selection{
		{Key: "triage-skill"},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if len(execution.Expansions) != 1 {
		t.Fatalf("expansions len = %d, want 1", len(execution.Expansions))
	}
	if service.Broker.Usage().ExpandedDefinitions != 1 {
		t.Fatalf("expanded definitions = %d, want 1", service.Broker.Usage().ExpandedDefinitions)
	}
}

func TestMaterializeRejectsSubAgentWithoutPlanOptIn(t *testing.T) {
	t.Parallel()

	service := Service{
		Broker: broker.New(
			testSnapshot(),
			catalog.BuiltinDefinitions(),
			budgets.Limits{
				Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
				Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
			},
		),
	}

	_, err := service.Materialize("project", []Selection{
		{Key: "triage-agent"},
	})
	if err == nil {
		t.Fatalf("Materialize() error = nil, want sub-agent denial")
	}
}

func testSnapshot() registry.Snapshot {
	return registry.Snapshot{
		Items: []registry.Item{
			{
				Kind:    registry.KindSkill,
				Key:     "triage-skill",
				Title:   "Triage Skill",
				Summary: "Classifies requests.",
				Sections: map[string]string{
					registry.SectionPurpose: "Decide the next action.",
				},
				Source: registry.SourceInfo{RelativePath: "skills/triage-skill.md"},
			},
			{
				Kind:    registry.KindAgent,
				Key:     "triage-agent",
				Title:   "Triage Agent",
				Summary: "Routes work.",
				Scopes:  []string{"project"},
				Sections: map[string]string{
					registry.SectionPurpose: "Route work deterministically.",
				},
				Source: registry.SourceInfo{RelativePath: "agents/triage-agent.md"},
			},
		},
	}
}
