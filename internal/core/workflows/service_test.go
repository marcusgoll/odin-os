package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"odin-os/internal/core/capabilities"
	"odin-os/internal/registry"
	"odin-os/internal/tools/catalog"
)

type recordingCapabilityGateway struct {
	descriptors map[string]capabilities.Descriptor
	getCalls    []getCapabilityCall
	invokeCalls []capabilities.InvokeRequest
}

type getCapabilityCall struct {
	id      string
	version string
}

func (gateway *recordingCapabilityGateway) GetCapability(id, version string) (capabilities.Descriptor, error) {
	gateway.getCalls = append(gateway.getCalls, getCapabilityCall{id: id, version: version})
	if descriptor, ok := gateway.descriptors[keyFor(id, version)]; ok {
		return descriptor, nil
	}
	return capabilities.Descriptor{}, fmt.Errorf("missing capability %s@%s", id, version)
}

func (gateway *recordingCapabilityGateway) InvokeCapability(_ context.Context, request capabilities.InvokeRequest) (capabilities.InvokeResponse, error) {
	gateway.invokeCalls = append(gateway.invokeCalls, request)
	return capabilities.InvokeResponse{
		RunID:  "run-1",
		Status: "completed",
		Output: json.RawMessage(`{"status":"ready"}`),
	}, nil
}

func TestWorkflowServiceResolvesCapabilityDependencies(t *testing.T) {
	t.Parallel()

	gateway := &recordingCapabilityGateway{
		descriptors: map[string]capabilities.Descriptor{
			keyFor("triage-skill", "1.0.0"): {
				Kind:         registry.KindSkill,
				Key:          "triage-skill",
				Name:         "triage-skill",
				Title:        "Triage Skill",
				Version:      "1.0.0",
				Summary:      "Classifies requests.",
				Availability: registry.Availability{Scope: "project"},
				InputSchema:  registry.SchemaRef{Type: "object"},
				OutputSchema: registry.SchemaRef{Type: "object"},
			},
			keyFor("triage-agent", "1.0.0"): {
				Kind:         registry.KindAgent,
				Key:          "triage-agent",
				Name:         "triage-agent",
				Title:        "Triage Agent",
				Version:      "1.0.0",
				Summary:      "Routes work.",
				Availability: registry.Availability{Scope: "project"},
			},
			keyFor("project.status", "1.0.0"): {
				Kind:         registry.KindCommand,
				Key:          "project.status",
				Name:         "project.status",
				Title:        "Project Status",
				Version:      "1.0.0",
				Summary:      "Reports project status.",
				Availability: registry.Availability{Scope: "project"},
				InputSchema:  registry.SchemaRef{Type: "object"},
				OutputSchema: registry.SchemaRef{Type: "object"},
			},
		},
	}

	service := NewService(gateway, catalog.BuiltinDefinitions())

	workflow := registry.Item{
		Key: "project-status-workflow",
		Dependencies: []registry.DependencyRef{
			{Kind: registry.KindSkill, Name: "triage-skill", Version: "1.0.0"},
			{Kind: registry.KindAgent, Name: "triage-agent", Version: "1.0.0"},
			{Kind: registry.KindCommand, Name: "project.status", Version: "1.0.0"},
		},
	}

	steps, err := service.Compile(workflow)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	steps = append(steps, Step{
		Capability:   "tool:task_list",
		VersionRange: "1.0.0",
		With: map[string]any{
			"scope": "project",
		},
	})

	result, err := service.Execute(context.Background(), steps)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(result.Steps) != 4 {
		t.Fatalf("Execute() steps = %d, want 4", len(result.Steps))
	}
	if result.Steps[0].Capability != "skill:triage-skill" {
		t.Fatalf("Execute()[0].Capability = %q, want skill:triage-skill", result.Steps[0].Capability)
	}
	if result.Steps[1].Capability != "agent:triage-agent" {
		t.Fatalf("Execute()[1].Capability = %q, want agent:triage-agent", result.Steps[1].Capability)
	}
	if result.Steps[2].Capability != "command:project.status" {
		t.Fatalf("Execute()[2].Capability = %q, want command:project.status", result.Steps[2].Capability)
	}
	if result.Steps[3].Capability != "tool:task_list" {
		t.Fatalf("Execute()[3].Capability = %q, want tool:task_list", result.Steps[3].Capability)
	}
	if result.Steps[2].Response == nil || result.Steps[2].Response.Status != "completed" {
		t.Fatalf("command step response = %+v, want completed response", result.Steps[2].Response)
	}
	if result.Steps[3].Tool == nil || result.Steps[3].Tool.Key != "task_list" {
		t.Fatalf("tool step = %+v, want task_list tool metadata", result.Steps[3].Tool)
	}
	if len(gateway.invokeCalls) != 1 {
		t.Fatalf("InvokeCapability() calls = %d, want 1", len(gateway.invokeCalls))
	}
	if got := gateway.invokeCalls[0]; got.CapabilityID != "project.status" || got.CapabilityVersion != "1.0.0" {
		t.Fatalf("InvokeCapability() call = %+v, want project.status 1.0.0", got)
	}
}

func TestWorkflowServiceRejectsMissingDependency(t *testing.T) {
	t.Parallel()

	service := NewService(&recordingCapabilityGateway{
		descriptors: map[string]capabilities.Descriptor{},
	}, catalog.BuiltinDefinitions())

	workflow := registry.Item{
		Key: "project-status-workflow",
		Dependencies: []registry.DependencyRef{
			{Kind: registry.KindSkill, Name: "triage-skill", Version: "1.0.0"},
		},
	}

	_, err := service.Compile(workflow)
	if err == nil {
		t.Fatal("Compile() error = nil, want dependency error")
	}

	var depErr *DependencyError
	if !errors.As(err, &depErr) {
		t.Fatalf("Compile() error = %v, want DependencyError", err)
	}
	if depErr.Capability != "skill:triage-skill" {
		t.Fatalf("DependencyError.Capability = %q, want skill:triage-skill", depErr.Capability)
	}
	if depErr.VersionRange != "1.0.0" {
		t.Fatalf("DependencyError.VersionRange = %q, want 1.0.0", depErr.VersionRange)
	}
}

func TestWorkflowServiceRejectsUnmarshalableCommandInput(t *testing.T) {
	t.Parallel()

	gateway := &recordingCapabilityGateway{
		descriptors: map[string]capabilities.Descriptor{
			keyFor("project.status", "1.0.0"): {
				Kind:         registry.KindCommand,
				Key:          "project.status",
				Name:         "project.status",
				Title:        "Project Status",
				Version:      "1.0.0",
				Summary:      "Reports project status.",
				Availability: registry.Availability{Scope: "project"},
				InputSchema:  registry.SchemaRef{Type: "object"},
				OutputSchema: registry.SchemaRef{Type: "object"},
			},
		},
	}

	service := NewService(gateway, catalog.BuiltinDefinitions())

	_, err := service.Execute(context.Background(), []Step{
		{
			Capability:   "command:project.status",
			VersionRange: "1.0.0",
			With: map[string]any{
				"bad": func() {},
			},
		},
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want marshaling error")
	}

	var depErr *DependencyError
	if !errors.As(err, &depErr) {
		t.Fatalf("Execute() error = %v, want DependencyError", err)
	}
	if depErr.Stage != "execute" {
		t.Fatalf("DependencyError.Stage = %q, want execute", depErr.Stage)
	}
	if len(gateway.invokeCalls) != 0 {
		t.Fatalf("InvokeCapability() calls = %d, want 0", len(gateway.invokeCalls))
	}
}

func keyFor(id, version string) string {
	return id + "@" + version
}
