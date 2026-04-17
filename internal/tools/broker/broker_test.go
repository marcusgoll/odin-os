package broker

import (
	"context"
	"testing"

	"odin-os/internal/registry"
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
	if len(cards) != 1 {
		t.Fatalf("Catalog() len = %d, want 1", len(cards))
	}
	if cards[0].Key != "echo-skill" {
		t.Fatalf("Catalog()[0].Key = %q, want %q", cards[0].Key, "echo-skill")
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

func testSnapshot() registry.Snapshot {
	return registry.Snapshot{
		Items: []registry.Item{
			{
				Kind:           registry.KindSkill,
				Key:            "triage-skill",
				Title:          "Triage Skill",
				Summary:        "Classifies requests.",
				Version:        "1.0.0",
				Enabled:        true,
				Scopes:         []string{"project"},
				Permissions:    []string{"repo.read"},
				HandlerType:    "command",
				HandlerRef:     "scripts/skills/triage-skill.sh",
				TimeoutSeconds: 15,
				InputSchema: map[string]any{
					"type": "object",
				},
				OutputSchema: map[string]any{
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

func testLimits() budgets.Limits {
	return budgets.Limits{
		Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
		Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
	}
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

func (invoker *stubBrokerToolInvoker) Invoke(_ context.Context, key string, request invocation.Request) (invocation.Result, error) {
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
