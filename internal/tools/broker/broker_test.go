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
		testSnapshot(),
		testBuiltins(),
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
		if card.Key == "huginn_visual_audit" {
			t.Fatalf("Catalog() exposed hidden legacy alias card: %+v", card)
		}
	}
}

func TestExpandReturnsFullSelectedDefinitionOnly(t *testing.T) {
	t.Parallel()

	broker := New(
		testSnapshot(),
		testBuiltins(),
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

func TestExpandReturnsCanonicalBuiltinDefinitionForAlias(t *testing.T) {
	t.Parallel()

	broker := New(
		testSnapshot(),
		testBuiltins(),
		budgets.Limits{
			Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
			Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
		},
	)

	expansion, err := broker.Expand("huginn_visual_audit")
	if err != nil {
		t.Fatalf("Expand(huginn_visual_audit) error = %v", err)
	}
	if expansion.Tool == nil {
		t.Fatal("Tool expansion = nil, want value")
	}
	if expansion.Tool.Key != "browser_visual_audit" {
		t.Fatalf("Tool.Key = %q, want browser_visual_audit", expansion.Tool.Key)
	}
	if expansion.Card.Key != "browser_visual_audit" {
		t.Fatalf("Card.Key = %q, want browser_visual_audit", expansion.Card.Key)
	}
}

func TestInvokeAndCompactRespectBudgets(t *testing.T) {
	t.Parallel()

	broker := New(
		testSnapshot(),
		testBuiltins(),
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

func TestInvokeToolBlocksApprovalRequiredPublicPublish(t *testing.T) {
	t.Parallel()

	invoked := false
	broker := New(
		testSnapshot(),
		map[string]catalog.ToolDefinition{
			"browser_x_post_publish": {
				Key:              "browser_x_post_publish",
				CanonicalKey:     "browser_x_post_publish",
				Title:            "Publish X Post",
				Summary:          "Publishes approved X content.",
				Scopes:           []string{"project"},
				Tags:             []string{"browser", "social", "publish"},
				BudgetCost:       1,
				SourceRef:        "builtin://browser_x_post_publish",
				RequiresApproval: true,
				ApprovalReason:   "public social publishing requires an approved social_outcome",
				Invoke: func(map[string]string) (catalog.StructuredResult, error) {
					invoked = true
					return catalog.StructuredResult{CapabilityKey: "browser_x_post_publish"}, nil
				},
			},
		},
		budgets.Limits{
			Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
			Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
		},
	)

	if _, err := broker.InvokeTool("browser_x_post_publish", map[string]string{"post_text": "publish"}); err == nil {
		t.Fatal("InvokeTool(browser_x_post_publish) error = nil, want approval-required refusal")
	} else if got := err.Error(); got != `tool "browser_x_post_publish" requires approval before invocation: public social publishing requires an approved social_outcome` {
		t.Fatalf("InvokeTool() error = %q", got)
	}
	if invoked {
		t.Fatal("approval-required tool invocation reached Invoke handler")
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

func testBuiltins() map[string]catalog.ToolDefinition {
	return map[string]catalog.ToolDefinition{
		"task_list": {
			Key:        "task_list",
			Title:      "Task List",
			Summary:    "Lists task projections.",
			Scopes:     []string{"project"},
			Tags:       []string{"runtime"},
			CostHint:   catalog.CostHintLow,
			BudgetCost: 1,
			SourceRef:  "builtin://task_list",
			Invoke: func(map[string]string) (catalog.StructuredResult, error) {
				return catalog.StructuredResult{
					CapabilityKey: "task_list",
					Summary:       "Task list prepared.",
					RawRef:        "builtin://task_list/result",
				}, nil
			},
		},
		"browser_visual_audit": {
			Key:          "browser_visual_audit",
			CanonicalKey: "browser_visual_audit",
			Title:        "Browser Visual Audit",
			Summary:      "Captures a live browser snapshot and screenshot for a visual review target.",
			Scopes:       []string{"global", "project"},
			Tags:         []string{"browser", "visual", "live"},
			CostHint:     catalog.CostHintMedium,
			BudgetCost:   2,
			SourceRef:    "builtin://browser_visual_audit",
			Invoke: func(map[string]string) (catalog.StructuredResult, error) {
				return catalog.StructuredResult{
					CapabilityKey: "browser_visual_audit",
					Summary:       "Captured browser visual audit evidence.",
					RawRef:        "builtin://browser_visual_audit/result",
				}, nil
			},
		},
		"huginn_visual_audit": {
			Key:          "huginn_visual_audit",
			CanonicalKey: "browser_visual_audit",
			Title:        "Browser Visual Audit",
			Summary:      "Captures a live browser snapshot and screenshot for a visual review target.",
			Hidden:       true,
			Scopes:       []string{"global", "project"},
			Tags:         []string{"browser", "visual", "live"},
			CostHint:     catalog.CostHintMedium,
			BudgetCost:   2,
			SourceRef:    "builtin://browser_visual_audit",
			Invoke: func(map[string]string) (catalog.StructuredResult, error) {
				return catalog.StructuredResult{
					CapabilityKey: "browser_visual_audit",
					Summary:       "Captured browser visual audit evidence.",
					RawRef:        "builtin://browser_visual_audit/result",
				}, nil
			},
		},
	}
}
