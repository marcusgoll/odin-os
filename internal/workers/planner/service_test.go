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

	context, err := service.Prepare(PrepareInput{
		Scope: "project",
		Workspace: WorkspaceContext{
			Key: "default",
		},
		Initiative: &InitiativeContext{
			Key:  "alpha",
			Kind: "managed_project",
		},
		Companion: &CompanionContext{
			Key:                "primary",
			Kind:               "assistant",
			ToolPolicyJSON:     `{"allow":["project_status"]}`,
			PlanningPolicyJSON: `{"mode":"guided"}`,
		},
		MemoryReferences: []MemoryReference{
			{Scope: "workspace", Summary: "Marcus prefers concise plans."},
		},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(context.Cards) == 0 {
		t.Fatalf("Prepare() cards len = 0, want > 0")
	}
	if context.Workspace.Key != "default" {
		t.Fatalf("Prepare().Workspace.Key = %q, want default", context.Workspace.Key)
	}
	if context.Companion == nil || context.Companion.Key != "primary" {
		t.Fatalf("Prepare().Companion = %+v, want primary companion", context.Companion)
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

	execution, err := service.Materialize(MaterializeInput{
		Scope: "project",
		Workspace: WorkspaceContext{
			Key: "default",
		},
		Initiative: &InitiativeContext{
			Key:  "alpha",
			Kind: "managed_project",
		},
		Companion: &CompanionContext{
			Key:                "primary",
			Kind:               "assistant",
			ToolPolicyJSON:     `{"allow":["project_status"]}`,
			PlanningPolicyJSON: `{"mode":"guided"}`,
		},
		Selections: []Selection{
			{Key: "triage-skill"},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if len(execution.Expansions) != 1 {
		t.Fatalf("expansions len = %d, want 1", len(execution.Expansions))
	}
	if execution.Workspace.Key != "default" {
		t.Fatalf("Materialize().Workspace.Key = %q, want default", execution.Workspace.Key)
	}
	if service.Broker.Usage().ExpandedDefinitions != 1 {
		t.Fatalf("expanded definitions = %d, want 1", service.Broker.Usage().ExpandedDefinitions)
	}
}

func TestMaterializeRejectsAgentRoleWithoutPlanOptIn(t *testing.T) {
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

	_, err := service.Materialize(MaterializeInput{
		Scope: "project",
		Workspace: WorkspaceContext{
			Key: "default",
		},
		Selections: []Selection{
			{Key: "triage-agent"},
		},
	})
	if err == nil {
		t.Fatalf("Materialize() error = nil, want agent-role denial")
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
				Role:    "intake-triager",
				Tools:   []string{"filesystem"},
				Sections: map[string]string{
					registry.SectionPurpose: "Route work deterministically.",
				},
				Source: registry.SourceInfo{RelativePath: "agents/triage-agent.md"},
			},
			{
				Kind:     registry.KindWorkflow,
				Key:      "project-intake",
				Title:    "Project Intake Workflow",
				Summary:  "Turns raw project work into bounded intake output.",
				Composes: []string{"triage-skill", "triage-agent"},
				Sections: map[string]string{
					registry.SectionPurpose: "Normalize project intake.",
				},
				Source: registry.SourceInfo{RelativePath: "workflows/project-intake.md"},
			},
			{
				Kind:    registry.KindCommand,
				Key:     "status-command",
				Title:   "Status Command",
				Summary: "Shows current runtime scope.",
				Command: "status",
				Aliases: []string{"stat"},
				Sections: map[string]string{
					registry.SectionPurpose: "Render runtime status.",
				},
				Source: registry.SourceInfo{RelativePath: "commands/status.md"},
			},
		},
	}
}
