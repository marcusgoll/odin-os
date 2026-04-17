package broker

import (
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
)

func TestCatalogReturnsThinCardsOnly(t *testing.T) {
	t.Parallel()

	broker := New(
		StaticSource(testSnapshot()),
		catalog.BuiltinDefinitions(),
		nil,
		budgets.Limits{
			Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
			Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
		},
	)

	cards, err := broker.Catalog("project")
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}
	if len(cards) == 0 {
		t.Fatalf("Catalog() len = 0, want > 0")
	}

	for _, card := range cards {
		if card.Key == "" || card.Title == "" || card.Summary == "" {
			t.Fatalf("thin card missing required fields: %+v", card)
		}
	}
}

func TestExpandReturnsFullSelectedDefinitionOnly(t *testing.T) {
	t.Parallel()

	broker := New(
		StaticSource(testSnapshot()),
		catalog.BuiltinDefinitions(),
		nil,
		budgets.Limits{
			Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
			Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
		},
	)

	expansion, err := broker.Expand("triage-skill")
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if expansion.Skill == nil {
		t.Fatalf("Skill expansion = nil, want value")
	}
	if expansion.Tool != nil || expansion.AgentRole != nil {
		t.Fatalf("unexpected expansion types: %+v", expansion)
	}
	if expansion.Skill.Sections[registry.SectionPurpose] == "" {
		t.Fatalf("skill sections missing purpose")
	}
}

func TestExpandReturnsWorkflowAgentRoleAndOperatorCommandDefinitions(t *testing.T) {
	t.Parallel()

	broker := New(
		StaticSource(testSnapshot()),
		catalog.BuiltinDefinitions(),
		nil,
		budgets.Limits{
			Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
			Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
		},
	)

	workflowExpansion, err := broker.Expand("project-intake")
	if err != nil {
		t.Fatalf("Expand(project-intake) error = %v", err)
	}
	if workflowExpansion.Workflow == nil {
		t.Fatalf("workflow expansion = nil, want value")
	}
	if len(workflowExpansion.Workflow.Composes) != 2 {
		t.Fatalf("workflow composes len = %d, want 2", len(workflowExpansion.Workflow.Composes))
	}

	agentRoleExpansion, err := broker.Expand("triage-agent")
	if err != nil {
		t.Fatalf("Expand(triage-agent) error = %v", err)
	}
	if agentRoleExpansion.AgentRole == nil {
		t.Fatalf("agent role expansion = nil, want value")
	}
	if agentRoleExpansion.AgentRole.Role == "" {
		t.Fatalf("agent role = %+v, want role", agentRoleExpansion.AgentRole)
	}

	commandExpansion, err := broker.Expand("status-command")
	if err != nil {
		t.Fatalf("Expand(status-command) error = %v", err)
	}
	if commandExpansion.OperatorCommand == nil {
		t.Fatalf("operator command expansion = nil, want value")
	}
	if commandExpansion.OperatorCommand.Command != "status" {
		t.Fatalf("operator command = %q, want status", commandExpansion.OperatorCommand.Command)
	}
}

func TestInvokeAndCompactRespectBudgets(t *testing.T) {
	t.Parallel()

	broker := New(
		StaticSource(testSnapshot()),
		catalog.BuiltinDefinitions(),
		nil,
		budgets.Limits{
			Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 1, MaxCostUnits: 10},
			Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 1, MaxCompactedBytes: 200},
		},
	)

	result, err := broker.InvokeTool("task_list", map[string]string{"scope": "project"})
	if err != nil {
		t.Fatalf("InvokeTool() error = %v", err)
	}
	if result.Summary == "" || result.RawRef == "" {
		t.Fatalf("structured result incomplete: %+v", result)
	}

	compacted, err := broker.Compact(result)
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if compacted.Summary == "" {
		t.Fatalf("compacted summary empty")
	}

	if _, err := broker.InvokeTool("task_list", map[string]string{"scope": "project"}); err == nil {
		t.Fatalf("second InvokeTool() error = nil, want budget denial")
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
				Tags:    []string{"intake"},
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
				Tags:    []string{"routing"},
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
