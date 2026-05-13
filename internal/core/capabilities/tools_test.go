package capabilities

import (
	"context"
	"encoding/json"
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/tools/catalog"
)

func TestBuiltinToolDescriptorsExposeVisibleTools(t *testing.T) {
	t.Parallel()

	descriptors := BuiltinToolDescriptors(map[string]catalog.ToolDefinition{
		"project_status": {
			Key:       "project_status",
			Title:     "Project Status",
			Summary:   "Summarizes managed project status for planning.",
			Scopes:    []string{"global", "project"},
			Tags:      []string{"project", "status"},
			SourceRef: "builtin://project_status",
			Schema:    map[string]any{"type": "object"},
		},
		"hidden_alias": {
			Key:          "hidden_alias",
			CanonicalKey: "project_status",
			Title:        "Hidden Alias",
			Hidden:       true,
			Scopes:       []string{"global"},
			Schema:       map[string]any{"type": "object"},
		},
	})

	descriptor, ok := descriptors["project_status"]
	if !ok {
		t.Fatalf("BuiltinToolDescriptors()[project_status] missing")
	}
	if descriptor.Kind != registry.KindTool {
		t.Fatalf("descriptor.Kind = %q, want %q", descriptor.Kind, registry.KindTool)
	}
	if descriptor.Version != "1.0.0" {
		t.Fatalf("descriptor.Version = %q, want 1.0.0", descriptor.Version)
	}
	if descriptor.Availability.Scope != "global" {
		t.Fatalf("descriptor.Availability.Scope = %q, want global", descriptor.Availability.Scope)
	}
	if len(descriptor.Scopes) != 2 || descriptor.Scopes[1] != "project" {
		t.Fatalf("descriptor.Scopes = %+v, want global and project", descriptor.Scopes)
	}
	if descriptor.InputSchema.Type != "object" {
		t.Fatalf("descriptor.InputSchema.Type = %q, want object", descriptor.InputSchema.Type)
	}
	if descriptor.Implementation.Kind != "builtin_tool" || descriptor.Implementation.Ref != "builtin://project_status" {
		t.Fatalf("descriptor.Implementation = %+v, want builtin tool ref", descriptor.Implementation)
	}
	if _, ok := descriptors["hidden_alias"]; ok {
		t.Fatalf("hidden alias descriptor was exposed")
	}
}

func TestRegistryDoesNotDefinePluginKind(t *testing.T) {
	t.Parallel()

	if registry.Kind("plugin").Valid() {
		t.Fatalf("registry.Kind(plugin).Valid() = true, want false")
	}
	if registry.Kind("plugin").IsInvokable() {
		t.Fatalf("registry.Kind(plugin).IsInvokable() = true, want false")
	}
}

func TestInvokeBuiltinToolCapabilityUsesCatalogDefinition(t *testing.T) {
	t.Parallel()

	definitions := map[string]catalog.ToolDefinition{
		"project_status": {
			Key:       "project_status",
			Scopes:    []string{"global"},
			SourceRef: "builtin://project_status",
			Schema:    map[string]any{"type": "object"},
			Invoke: func(input map[string]string) (catalog.StructuredResult, error) {
				if input["project_key"] != "alpha-cli" {
					t.Fatalf("input[project_key] = %q, want alpha-cli", input["project_key"])
				}
				return catalog.StructuredResult{
					CapabilityKey: "project_status",
					Summary:       "Project status prepared for alpha-cli.",
					KeyFacts:      map[string]string{"project_key": "alpha-cli"},
					RawRef:        "builtin://project_status/result",
				}, nil
			},
		},
	}
	descriptor := BuiltinToolDescriptors(definitions)["project_status"]

	response, err := InvokeBuiltinToolCapability(context.Background(), definitions, InvokeRequest{
		CapabilityID:      "project_status",
		CapabilityVersion: "1.0.0",
		Input:             json.RawMessage(`{"project_key":"alpha-cli"}`),
	}, descriptor)
	if err != nil {
		t.Fatalf("InvokeBuiltinToolCapability() error = %v", err)
	}
	if response.Status != "completed" {
		t.Fatalf("response.Status = %q, want completed", response.Status)
	}
	if response.RunID != "builtin://project_status/result" {
		t.Fatalf("response.RunID = %q, want raw ref", response.RunID)
	}
	if string(response.Output) == "" || !json.Valid(response.Output) {
		t.Fatalf("response.Output = %q, want valid JSON", string(response.Output))
	}
}

func TestInvokeBuiltinToolCapabilityBlocksApprovalRequiredTool(t *testing.T) {
	t.Parallel()

	invoked := false
	definitions := map[string]catalog.ToolDefinition{
		"browser_x_post_publish": {
			Key:              "browser_x_post_publish",
			Scopes:           []string{"project"},
			SourceRef:        "builtin://browser_x_post_publish",
			Schema:           map[string]any{"type": "object"},
			RequiresApproval: true,
			ApprovalReason:   "public social publishing requires an approved social_outcome",
			Invoke: func(map[string]string) (catalog.StructuredResult, error) {
				invoked = true
				return catalog.StructuredResult{CapabilityKey: "browser_x_post_publish"}, nil
			},
		},
	}
	descriptor := BuiltinToolDescriptors(definitions)["browser_x_post_publish"]

	_, err := InvokeBuiltinToolCapability(context.Background(), definitions, InvokeRequest{
		CapabilityID:      "browser_x_post_publish",
		CapabilityVersion: "1.0.0",
		Input:             json.RawMessage(`{"post_text":"publish"}`),
	}, descriptor)
	if err == nil {
		t.Fatal("InvokeBuiltinToolCapability() error = nil, want approval-required refusal")
	}
	code, ok := errorCode(err)
	if !ok {
		t.Fatalf("InvokeBuiltinToolCapability() error = %v, want coded policy error", err)
	}
	if code != "approval_required" {
		t.Fatalf("InvokeBuiltinToolCapability() code = %q, want approval_required", code)
	}
	if invoked {
		t.Fatal("approval-required builtin tool invocation reached Invoke handler")
	}
}
