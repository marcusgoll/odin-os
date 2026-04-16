package mcp

import (
	"context"
	"testing"

	"odin-os/internal/core/capabilities"
	"odin-os/internal/registry"
)

type testCapabilitySource struct {
	cards map[registry.Kind][]capabilities.CapabilityCard
	items map[string]capabilities.Descriptor
}

func (source testCapabilitySource) ListCapabilities(kind registry.Kind, scope string) []capabilities.CapabilityCard {
	return append([]capabilities.CapabilityCard(nil), source.cards[kind]...)
}

func (source testCapabilitySource) GetCapability(id, version string) (capabilities.Descriptor, error) {
	return source.items[id+"@"+version], nil
}

func TestMCPListsCapabilitiesAsTools(t *testing.T) {
	t.Parallel()

	server := NewServer(testCapabilitySource{
		cards: map[registry.Kind][]capabilities.CapabilityCard{
			registry.KindCommand: {
				{ID: "project.status", Kind: registry.KindCommand, Name: "Project Status", Version: "1.2.3", Scope: "project"},
			},
			registry.KindAgent: {
				{ID: "planner.agent", Kind: registry.KindAgent, Name: "Planner Agent", Version: "1.0.0", Scope: "project"},
			},
		},
		items: map[string]capabilities.Descriptor{
			"project.status@1.2.3": {
				Kind:         registry.KindCommand,
				Key:          "project.status",
				Name:         "Project Status",
				Version:      "1.2.3",
				Availability: registry.Availability{Scope: "project"},
				Permissions:  []string{"workspace.read"},
				InputSchema:  registry.SchemaRef{Type: "object"},
				OutputSchema: registry.SchemaRef{Type: "object"},
			},
			"planner.agent@1.0.0": {
				Kind:         registry.KindAgent,
				Key:          "planner.agent",
				Name:         "Planner Agent",
				Version:      "1.0.0",
				Availability: registry.Availability{Scope: "project"},
			},
		},
	})

	tools, err := server.ListTools(context.Background(), "project")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ListTools() len = %d, want 1", len(tools))
	}

	tool := tools[0]
	if tool.CapabilityID != "project.status" {
		t.Fatalf("ListTools()[0].CapabilityID = %q, want %q", tool.CapabilityID, "project.status")
	}
	if tool.Kind != registry.KindCommand {
		t.Fatalf("ListTools()[0].Kind = %q, want %q", tool.Kind, registry.KindCommand)
	}
	if tool.InputSchema.Type != "object" {
		t.Fatalf("ListTools()[0].InputSchema.Type = %q, want %q", tool.InputSchema.Type, "object")
	}
	if tool.OutputSchema.Type != "object" {
		t.Fatalf("ListTools()[0].OutputSchema.Type = %q, want %q", tool.OutputSchema.Type, "object")
	}
	if len(tool.Permissions) != 1 || tool.Permissions[0] != "workspace.read" {
		t.Fatalf("ListTools()[0].Permissions = %+v, want workspace.read", tool.Permissions)
	}
}
