package broker

import (
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/registry/compiler"
	"odin-os/internal/registry/parser"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
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
		catalog.BuiltinDefinitions(),
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

func TestBrokerUsesNormalizedCapabilitySnapshot(t *testing.T) {
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
	foundWorkflow := false
	for _, card := range cards {
		if card.Key != "project-status-workflow" {
			continue
		}
		foundWorkflow = true
		if card.Kind != catalog.KindWorkflow {
			t.Fatalf("workflow card kind = %q, want %q", card.Kind, catalog.KindWorkflow)
		}
	}
	if !foundWorkflow {
		t.Fatal("Catalog() missing project-status-workflow card")
	}

	expansion, err := broker.Expand("project-status-workflow")
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if expansion.Workflow == nil {
		t.Fatal("workflow expansion = nil, want value")
	}
	if expansion.Workflow.Version != "1.0.0" {
		t.Fatalf("workflow version = %q, want 1.0.0", expansion.Workflow.Version)
	}
	if expansion.Workflow.SourceRef != "internal/registry/testdata/normalized/workflow-project-status.md" {
		t.Fatalf("workflow source ref = %q, want normalized snapshot path", expansion.Workflow.SourceRef)
	}
	if len(expansion.Workflow.Dependencies) != 3 {
		t.Fatalf("workflow dependencies = %d, want 3", len(expansion.Workflow.Dependencies))
	}
	if expansion.Workflow.Dependencies[0].Kind != registry.KindSkill || expansion.Workflow.Dependencies[0].Name != "triage-skill" {
		t.Fatalf("workflow dependency[0] = %+v, want triage-skill", expansion.Workflow.Dependencies[0])
	}
	if expansion.Workflow.Dependencies[1].Kind != registry.Kind("tool") || expansion.Workflow.Dependencies[1].Name != "project_status" {
		t.Fatalf("workflow dependency[1] = %+v, want project_status tool", expansion.Workflow.Dependencies[1])
	}
	if expansion.Workflow.Dependencies[2].Kind != registry.KindCommand || expansion.Workflow.Dependencies[2].Name != "project.status" {
		t.Fatalf("workflow dependency[2] = %+v, want project.status", expansion.Workflow.Dependencies[2])
	}
}

func TestCatalogHonorsNormalizedWorkflowScope(t *testing.T) {
	t.Parallel()

	snapshot := compiledWorkflowSnapshot(t)
	broker := New(snapshot, nil, budgets.Limits{
		Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
		Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
	})

	projectCards := broker.Catalog("project")
	foundProjectWorkflow := false
	for _, card := range projectCards {
		if card.Key == "project-status-workflow" {
			foundProjectWorkflow = true
			break
		}
	}
	if !foundProjectWorkflow {
		t.Fatal("Catalog(project) missing project-status-workflow card")
	}

	globalCards := broker.Catalog("global")
	for _, card := range globalCards {
		if card.Key == "project-status-workflow" {
			t.Fatalf("Catalog(global) unexpectedly included project-status-workflow card: %+v", card)
		}
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
			{
				Kind:    registry.KindWorkflow,
				Key:     "project-status-workflow",
				Title:   "Project Status Workflow",
				Summary: "Coordinates project intake and status gathering.",
				Scopes:  []string{"project"},
				Tags:    []string{"projects", "status"},
				Version: "1.0.0",
				Dependencies: []registry.DependencyRef{
					{Kind: registry.KindSkill, Name: "triage-skill", Version: "1.0.0"},
					{Kind: registry.Kind("tool"), Name: "project_status", Version: "1.0.0"},
					{Kind: registry.KindCommand, Name: "project.status", Version: "1.0.0"},
				},
				Composes: []string{"triage-skill", "triage-agent"},
				Sections: map[string]string{
					registry.SectionPurpose:         "Coordinate intake and status gathering for a project.",
					registry.SectionWhenToUse:       "Use this workflow when project context needs to be re-established.",
					registry.SectionInputs:          "Request details, current project metadata, and runtime checkpoints.",
					registry.SectionProcedure:       "Classify the request, gather context, and produce a next-step plan.",
					registry.SectionOutputs:         "A normalized intake result and any governance requirements.",
					registry.SectionConstraints:     "Preserve project policy and avoid unnecessary mutation.",
					registry.SectionSuccessCriteria: "The workflow yields a reproducible next-step plan.",
				},
				Source: registry.SourceInfo{RelativePath: "internal/registry/testdata/normalized/workflow-project-status.md"},
			},
		},
	}
}

func compiledWorkflowSnapshot(t *testing.T) registry.Snapshot {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("..", "..", "..", "registry", "workflows", "project-status.md"))
	if err != nil {
		t.Fatalf("ReadFile(project-status workflow) error = %v", err)
	}

	document, diagnostics := parser.ParseSource(registry.SourceFile{
		Path:         filepath.Join("/tmp", "project-status.md"),
		RelativePath: filepath.ToSlash(filepath.Join("registry", "workflows", "project-status.md")),
		ExpectedKind: registry.KindWorkflow,
	}, content)
	if len(diagnostics) != 0 {
		t.Fatalf("ParseSource(project-status workflow) diagnostics = %v, want none", diagnostics)
	}

	snapshot := compiler.Compile([]registry.ParsedDocument{document}, nil)
	if len(snapshot.Diagnostics) != 0 {
		t.Fatalf("Compile(project-status workflow) diagnostics = %v, want none", snapshot.Diagnostics)
	}
	return snapshot
}
