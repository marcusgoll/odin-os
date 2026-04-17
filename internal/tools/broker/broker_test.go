package broker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/registry/compiler"
	"odin-os/internal/registry/parser"
	"odin-os/internal/skills"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
	"odin-os/internal/tools/invocation"
)

func TestCatalogReloadsRegistryStateOnEachCall(t *testing.T) {
	t.Parallel()

	source := &stubSource{}
	broker := New(
		source,
		catalog.BuiltinDefinitions(),
		nil,
		testLimits(),
	)

	source.snapshot = registry.Snapshot{
		Items: []registry.Item{
			{
				Kind:        registry.KindSkill,
				Key:         "echo-skill",
				Title:       "Echo Skill",
				Summary:     "Echoes requests.",
				Version:     "1.0.0",
				Enabled:     true,
				Scopes:      []string{"project"},
				Permissions: []string{"repo.read"},
				HandlerType: "command",
				HandlerRef:  "scripts/skills/echo-skill.sh",
				Sections: map[string]string{
					registry.SectionPurpose: "Echo input.",
				},
				Source: registry.SourceInfo{RelativePath: "skills/echo-skill.md"},
			},
		},
	}

	cards, err := broker.Catalog("project")
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}

	found := false
	for _, card := range cards {
		if card.Key == "echo-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Catalog() cards = %+v, want echo-skill entry", cards)
	}
}

func TestExpandReturnsFullSelectedDefinitionOnly(t *testing.T) {
	t.Parallel()

	broker := New(
		StaticSource(testSnapshot()),
		catalog.BuiltinDefinitions(),
		nil,
		testLimits(),
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
	if expansion.Skill.HandlerType != "command" {
		t.Fatalf("skill handler type = %q, want command", expansion.Skill.HandlerType)
	}
}

func TestInvokeAndCompactRespectBudgets(t *testing.T) {
	t.Parallel()

	broker := New(
		StaticSource(testSnapshot()),
		catalog.BuiltinDefinitionsWithInvoker(&stubBrokerToolInvoker{
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
		nil,
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

func TestBrokerUsesNormalizedCapabilitySnapshot(t *testing.T) {
	t.Parallel()

	broker := New(
		StaticSource(testSnapshot()),
		catalog.BuiltinDefinitions(),
		nil,
		testLimits(),
	)

	cards, err := broker.Catalog("project")
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}

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
	broker := New(
		StaticSource(snapshot),
		nil,
		nil,
		testLimits(),
	)

	projectCards, err := broker.Catalog("project")
	if err != nil {
		t.Fatalf("Catalog(project) error = %v", err)
	}
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

	globalCards, err := broker.Catalog("global")
	if err != nil {
		t.Fatalf("Catalog(global) error = %v", err)
	}
	for _, card := range globalCards {
		if card.Key == "project-status-workflow" {
			t.Fatalf("Catalog(global) unexpectedly included project-status-workflow card: %+v", card)
		}
	}
}

func TestInvokeSkillUsesRegistryBackedInvoker(t *testing.T) {
	t.Parallel()

	invoker := &stubSkillInvoker{
		response: skills.InvokeResponse{
			Status:    "ok",
			Summary:   "echo complete",
			Output:    map[string]any{"message": "hello"},
			RawRef:    "skill://echo",
			RawOutput: `{"message":"hello"}`,
		},
	}

	broker := New(
		StaticSource(testSnapshot()),
		catalog.BuiltinDefinitions(),
		invoker,
		budgets.Limits{
			Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 1, MaxCostUnits: 10},
			Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
		},
	)

	ctx := context.WithValue(context.Background(), brokerContextKey("request_id"), "abc123")
	result, err := broker.InvokeSkill(ctx, skills.InvokeRequest{
		Key:   "triage-skill",
		Input: map[string]any{"message": "hello"},
		Context: skills.InvocationContext{
			ResolvedScopeKind: "project",
			Project: &skills.InvocationProject{
				ID:  7,
				Key: "alpha",
			},
		},
	})
	if err != nil {
		t.Fatalf("InvokeSkill() error = %v", err)
	}
	if result.CapabilityKey != "triage-skill" {
		t.Fatalf("CapabilityKey = %q, want triage-skill", result.CapabilityKey)
	}
	if result.KeyFacts["message"] != "hello" {
		t.Fatalf("message key fact = %q, want hello", result.KeyFacts["message"])
	}
	if got := invoker.lastRequest.Context.Project.Key; got != "alpha" {
		t.Fatalf("invoked project key = %q, want alpha", got)
	}
	if got := invoker.lastRequest.Context.ResolvedScopeKind; got != "project" {
		t.Fatalf("invoked scope = %q, want project", got)
	}
	if got := invoker.lastContext.Value(brokerContextKey("request_id")); got != "abc123" {
		t.Fatalf("ctx request_id = %v, want abc123", got)
	}
}

func TestCompactPreservesStructuredResultSource(t *testing.T) {
	t.Parallel()

	broker := New(
		StaticSource(testSnapshot()),
		catalog.BuiltinDefinitions(),
		nil,
		testLimits(),
	)

	compacted, err := broker.Compact(catalog.StructuredResult{
		CapabilityKey: "triage-skill",
		Source:        "skill",
		Summary:       "echo complete",
	})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if compacted.Source != "skill" {
		t.Fatalf("compacted source = %q, want skill", compacted.Source)
	}
}

func testSnapshot() registry.Snapshot {
	return registry.Snapshot{
		Items: []registry.Item{
			{
				Kind:           registry.KindSkill,
				Key:            "triage-skill",
				Name:           "triage-skill",
				Title:          "Triage Skill",
				Summary:        "Classifies requests.",
				Version:        "1.0.0",
				Enabled:        true,
				Scopes:         []string{"project"},
				Permissions:    []string{"repo.read"},
				HandlerType:    "command",
				HandlerRef:     "scripts/skills/triage-skill.sh",
				TimeoutSeconds: 15,
				LegacyInputSchema: map[string]any{
					"type": "object",
				},
				LegacyOutputSchema: map[string]any{
					"type": "object",
				},
				Tags: []string{"intake"},
				Sections: map[string]string{
					registry.SectionPurpose: "Decide the next action.",
				},
				Source: registry.SourceInfo{RelativePath: "skills/triage-skill.md"},
			},
			{
				Kind:    registry.KindAgent,
				Key:     "triage-agent",
				Name:    "triage-agent",
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
				APIVersion: registry.NormalizedAPIVersion,
				Kind:       registry.KindWorkflow,
				Key:        "project-status-workflow",
				Name:       "project-status-workflow",
				Title:      "Project Status Workflow",
				Summary:    "Coordinates project intake and status gathering.",
				Scopes:     []string{"project"},
				Tags:       []string{"projects", "status"},
				Version:    "1.0.0",
				Availability: registry.Availability{
					Scope: "project",
				},
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

func testLimits() budgets.Limits {
	return budgets.Limits{
		Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
		Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
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

type stubSource struct {
	snapshot registry.Snapshot
}

func (source *stubSource) LoadSnapshot() (registry.Snapshot, error) {
	return source.snapshot, nil
}

type stubBrokerToolInvoker struct {
	result invocation.Result
}

func (invoker *stubBrokerToolInvoker) Invoke(_ context.Context, key string, _ invocation.Request) (invocation.Result, error) {
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

type stubSkillInvoker struct {
	response    skills.InvokeResponse
	lastContext context.Context
	lastRequest skills.InvokeRequest
}

func (invoker *stubSkillInvoker) Invoke(ctx context.Context, request skills.InvokeRequest) (skills.InvokeResponse, error) {
	invoker.lastContext = ctx
	invoker.lastRequest = request
	response := invoker.response
	if response.SkillKey == "" {
		response.SkillKey = request.Key
	}
	return response, nil
}

type brokerContextKey string
