package planner

import (
	"context"
	"testing"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	"odin-os/internal/skills"
	"odin-os/internal/tools/broker"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/tools/catalog"
)

func TestPrepareStartsFromThinCatalogOnly(t *testing.T) {
	t.Parallel()

	service := Service{
		Broker: broker.New(
			broker.StaticSource(testSnapshot()),
			catalog.BuiltinDefinitions(),
			nil,
			testLimits(),
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
			broker.StaticSource(testSnapshot()),
			catalog.BuiltinDefinitions(),
			nil,
			testLimits(),
		),
	}

	execution, err := service.Materialize(context.Background(), "project", skills.InvocationContext{ResolvedScopeKind: "project"}, []Selection{
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

func TestMaterializeInvokesRegistryBackedSkill(t *testing.T) {
	t.Parallel()

	invoker := &stubSkillInvoker{
		response: skills.InvokeResponse{
			Status:    "ok",
			Summary:   "triage complete",
			Output:    map[string]any{"message": "hello"},
			RawRef:    "skill://triage",
			RawOutput: `{"message":"hello"}`,
		},
	}

	service := Service{
		Broker: broker.New(
			broker.StaticSource(testSnapshot()),
			catalog.BuiltinDefinitions(),
			invoker,
			testLimits(),
		),
	}

	ctx := context.WithValue(context.Background(), plannerContextKey("request_id"), "planner-1")
	invocationContext := skills.InvocationContext{
		ResolvedScopeKind: "project",
		Project: &skills.InvocationProject{
			ID:  42,
			Key: "alpha",
		},
		Manifest: projects.Manifest{Key: "alpha"},
	}

	execution, err := service.Materialize(ctx, "project", invocationContext, []Selection{
		{
			Key:         "triage-skill",
			InvokeSkill: true,
			SkillInput:  map[string]any{"message": "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if len(execution.Compacted) != 1 {
		t.Fatalf("compacted len = %d, want 1", len(execution.Compacted))
	}
	if execution.Compacted[0].CapabilityKey != "triage-skill" {
		t.Fatalf("CapabilityKey = %q, want triage-skill", execution.Compacted[0].CapabilityKey)
	}
	if got := invoker.lastRequest.Context.Project.Key; got != "alpha" {
		t.Fatalf("invoked project key = %q, want alpha", got)
	}
	if got := invoker.lastRequest.Context.Manifest.Key; got != "alpha" {
		t.Fatalf("invoked manifest key = %q, want alpha", got)
	}
	if got := invoker.lastContext.Value(plannerContextKey("request_id")); got != "planner-1" {
		t.Fatalf("ctx request_id = %v, want planner-1", got)
	}
}

func TestMaterializeRejectsSubAgentWithoutPlanOptIn(t *testing.T) {
	t.Parallel()

	service := Service{
		Broker: broker.New(
			broker.StaticSource(testSnapshot()),
			catalog.BuiltinDefinitions(),
			nil,
			testLimits(),
		),
	}

	_, err := service.Materialize(context.Background(), "project", skills.InvocationContext{ResolvedScopeKind: "project"}, []Selection{
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

func testLimits() budgets.Limits {
	return budgets.Limits{
		Tool:    budgets.Tool{MaxSelections: 10, MaxInvocations: 10, MaxCostUnits: 20},
		Context: budgets.Context{MaxExpandedDefinitions: 10, MaxCompactedResults: 10, MaxCompactedBytes: 1000},
	}
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

type plannerContextKey string
