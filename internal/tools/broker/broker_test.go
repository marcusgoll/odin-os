package broker

import (
	"context"
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
	"odin-os/internal/tools/invocation"
)

func TestCatalogReturnsThinCardsOnly(t *testing.T) {
	t.Parallel()

	broker := New(
		testSnapshot(),
		catalog.BuiltinDefinitions(),
		budgets.Limits{
			Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
			Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
		},
	)

	cards := broker.Catalog("project")
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
		testSnapshot(),
		catalog.BuiltinDefinitions(),
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
	if expansion.Tool != nil || expansion.SubAgent != nil {
		t.Fatalf("unexpected expansion types: %+v", expansion)
	}
	if expansion.Skill.Sections[registry.SectionPurpose] == "" {
		t.Fatalf("skill sections missing purpose")
	}
}

func TestInvokeAndCompactRespectBudgets(t *testing.T) {
	t.Parallel()

	broker := New(
		testSnapshot(),
		catalog.BuiltinDefinitionsWithInvoker(&stubBrokerInvoker{
			result: invocation.Result{
				Source:  "script",
				Summary: "Project alpha status from runtime.",
				KeyFacts: map[string]string{
					"project_key": "alpha",
				},
				RawRef:    "driver://project_status/alpha",
				RawOutput: "project=alpha",
			},
		}),
		budgets.Limits{
			Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 1, MaxCostUnits: 10},
			Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 1, MaxCompactedBytes: 200},
		},
	)

	result, err := broker.InvokeTool("project_status", map[string]string{"project_key": "alpha"})
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

	if _, err := broker.InvokeTool("project_status", map[string]string{"project_key": "alpha"}); err == nil {
		t.Fatalf("second InvokeTool() error = nil, want budget denial")
	}
}

func TestCompactPreservesStructuredResultSource(t *testing.T) {
	t.Parallel()

	broker := New(
		testSnapshot(),
		catalog.BuiltinDefinitionsWithInvoker(&stubBrokerInvoker{
			result: invocation.Result{
				Source: "script",
			},
		}),
		budgets.Limits{
			Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
			Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
		},
	)

	result, err := broker.InvokeTool("project_status", map[string]string{"project_key": "alpha"})
	if err != nil {
		t.Fatalf("InvokeTool(project_status) error = %v", err)
	}
	if result.Source != "driver" {
		t.Fatalf("result.Source = %q, want driver", result.Source)
	}

	compacted, err := broker.Compact(result)
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if compacted.Source != "driver" {
		t.Fatalf("compacted.Source = %q, want driver", compacted.Source)
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
				Sections: map[string]string{
					registry.SectionPurpose: "Route work deterministically.",
				},
				Source: registry.SourceInfo{RelativePath: "agents/triage-agent.md"},
			},
		},
	}
}

type stubBrokerInvoker struct {
	result invocation.Result
}

func (invoker *stubBrokerInvoker) Invoke(_ context.Context, key string, request invocation.Request) (invocation.Result, error) {
	if key != "project_status" {
		return invocation.Result{}, nil
	}
	result := invoker.result
	if result.KeyFacts == nil {
		result.KeyFacts = map[string]string{}
	}
	if result.Summary == "" {
		result.Summary = "runtime-backed"
	}
	if result.RawRef == "" {
		result.RawRef = "driver://project_status/alpha"
	}
	if result.RawOutput == "" {
		result.RawOutput = "project=alpha open_tasks=1"
	}
	if result.Source == "" {
		result.Source = "script"
	}
	return result, nil
}
